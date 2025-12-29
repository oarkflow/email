package main

import (
	"log"
	"sort"
)

// SchedulerOptimizer is pluggable interface for allocating providers for a batch of jobs.
type SchedulerOptimizer interface {
	// AllocateJobs returns a map jobID->provider chosen for that job.
	AllocateJobs(jobs []*ScheduledEmail) map[string]string
}

// GreedyBatchOptimizer is a simple optimizer that assigns providers per-job using
// per-job routing candidates and respects per-route provider capacities for the batch.
// It prefers providers ordered by recency-weighted score (via resolveProviders) and
// enforces per-batch capacity limits.
type GreedyBatchOptimizer struct {
}

func (o *GreedyBatchOptimizer) AllocateJobs(jobs []*ScheduledEmail) map[string]string {
	// group jobs by number of candidates to assign constrained jobs first
	type jobWrap struct {
		job   *ScheduledEmail
		cands []string
	}
	wrapped := make([]jobWrap, 0, len(jobs))
	for _, j := range jobs {
		// derive candidates without applying route rate-limit checks (optimizer handles allocation)
		var c []string
		if len(j.Config.ProviderPriority) > 0 {
			c = append(c, j.Config.ProviderPriority...)
		} else {
			if r := findFirstMatchingRoute(j.Config); r != nil {
				if len(r.ProviderPriority) > 0 {
					c = append(c, r.ProviderPriority...)
				} else if r.Provider != "" {
					c = append(c, r.Provider)
				}
			}
			if len(c) == 0 && j.Config.Provider != "" {
				c = append(c, j.Config.Provider)
			}
		}
		if len(c) == 0 {
			// nothing to try
			wrapped = append(wrapped, jobWrap{job: j, cands: c})
			continue
		}
		// reorder candidates by usage/cost/capacity in certain cases:
		// - if the route provides cost/capacity/weight hints (they should influence ordering even when ProviderPriority is set)
		// - or if the candidates were derived from the route (no explicit ProviderPriority)
		if len(c) > 1 {
			if r := findFirstMatchingRoute(j.Config); r != nil {
				// If the route provides strong hints (weights or costs), reorder by usage/cost/capacity
				if len(r.ProviderWeights) > 0 || len(r.ProviderCostOverrides) > 0 {
					log.Printf("optimizer: job=%s initial candidates=%v route=%+v", j.ID, c, r)
					c = sortProvidersByUsage(c, r.ToDomains, r.SelectionWindow, r.ProviderWeights, r.RecencyHalfLife, r.ProviderCapacities, r.ProviderCostOverrides)
					log.Printf("optimizer: job=%s ordered candidates=%v", j.ID, c)
					// If ProviderPriority is not set on the config, we already reordered; otherwise we respect the explicit list unless costs/weights are present
				} else if len(j.Config.ProviderPriority) == 0 {
					// no explicit provider priority - use route priority if present
					if len(r.ProviderPriority) > 0 {
						// filter route priority to those present in c
						newc := make([]string, 0, len(r.ProviderPriority))
						for _, p := range r.ProviderPriority {
							for _, existing := range c {
								if p == existing {
									newc = append(newc, p)
									break
								}
							}
						}
						// append any leftover candidates
						for _, existing := range c {
							found := false
							for _, nc := range newc {
								if existing == nc {
									found = true
									break
								}
							}
							if !found {
								newc = append(newc, existing)
							}
						}
						c = newc
					} else {
						c = sortProvidersByUsage(c, nil, 0, nil, 0, nil, nil)
					}
				} else if len(r.ProviderPriority) > 0 && len(j.Config.ProviderPriority) > 0 {
					// both route and config set priorities - prefer the route's declared order, but keep only providers present in c
					newc := make([]string, 0, len(r.ProviderPriority))
					for _, p := range r.ProviderPriority {
						for _, existing := range c {
							if p == existing {
								newc = append(newc, p)
								break
							}
						}
					}
					for _, existing := range c {
						found := false
						for _, nc := range newc {
							if existing == nc {
								found = true
								break
							}
						}
						if !found {
							newc = append(newc, existing)
						}
					}
					c = newc
				}
			} else if len(j.Config.ProviderPriority) == 0 {
				c = sortProvidersByUsage(c, nil, 0, nil, 0, nil, nil)
			}
		}
		wrapped = append(wrapped, jobWrap{job: j, cands: c})
	}
	// sort by candidate count asc
	sort.Slice(wrapped, func(i, j int) bool {
		if len(wrapped[i].cands) == len(wrapped[j].cands) {
			return wrapped[i].job.ID < wrapped[j].job.ID
		}
		return len(wrapped[i].cands) < len(wrapped[j].cands)
	})

	assign := map[string]string{}
	counts := map[string]int{}

	for _, w := range wrapped {
		chosen := ""
		// find route for this job to access per-route capacities
		route := findFirstMatchingRoute(w.job.Config)
		// debug
		//log.Printf("optimizer job=%s candidates=%v route=%+v", w.job.ID, w.cands, route)
		// pick eligible providers (those under capacity) and choose the best by remaining capacity then cost
		eligible := make([]string, 0, len(w.cands))
		remCap := map[string]int{}
		for _, prov := range w.cands {
			cap := -1 // -1 means unlimited
			if route != nil {
				if v, ok := route.ProviderCapacities[prov]; ok && v > 0 {
					cap = v
				}
			}
			if cap < 0 {
				if ds, ok := providerDefaults[prov]; ok && ds.Capacity > 0 {
					cap = ds.Capacity
				}
			}
			if cap < 0 {
				// unlimited - treat as very large cap
				remCap[prov] = 1 << 30
				eligible = append(eligible, prov)
				continue
			}
			rem := cap - counts[prov]
			if rem > 0 {
				remCap[prov] = rem
				eligible = append(eligible, prov)
			}
		}
		if len(eligible) > 0 {
			// prefer the highest-ranked candidate that still has remaining capacity
			chosen = ""
			for _, prov := range w.cands {
				if remCap[prov] > 0 {
					chosen = prov
					break
				}
			}
			// if none of the ranked candidates have capacity (shouldn't happen since eligible>0),
			// fall back to provider with largest remaining capacity, tie-break by lower cost
			if chosen == "" {
				best := eligible[0]
				bestRem := remCap[best]
				bestCost := 1.0
				if ds, ok := providerDefaults[best]; ok && ds.Cost > 0 {
					bestCost = ds.Cost
				}
				for _, prov := range eligible[1:] {
					rem := remCap[prov]
					cost := 1.0
					if ds, ok := providerDefaults[prov]; ok && ds.Cost > 0 {
						cost = ds.Cost
					}
					if rem > bestRem || (rem == bestRem && cost < bestCost) {
						best = prov
						bestRem = rem
						bestCost = cost
					}
				}
				chosen = best
			}
		} else {
			// no provider under capacity found; pick first candidate (best effort)
			if len(w.cands) > 0 {
				chosen = w.cands[0]
			} else {
				log.Printf("optimizer: no candidates for job %s", w.job.ID)
				continue
			}
		}
		if chosen == "" {
			// no provider under capacity found; pick first candidate (best effort)
			if len(w.cands) > 0 {
				chosen = w.cands[0]
			} else {
				log.Printf("optimizer: no candidates for job %s", w.job.ID)
				continue
			}
		}
		assign[w.job.ID] = chosen
		counts[chosen]++
		log.Printf("optimizer: assigned job=%s provider=%s counts=%v", w.job.ID, chosen, counts)
	}
	return assign
}
