// Package resilience holds the L5 resilience scenarios (build tag `resilience`,
// run by `make resilience`). The onBOARD client is driven in-process against the
// compose gateway through a Toxiproxy proxy that shapes the network; assertions
// read server state and scrape /metrics — never sleep-and-pray (D-017/D-018).
package resilience
