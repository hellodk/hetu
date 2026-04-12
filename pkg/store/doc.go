// Package store provides config-driven persistence helpers for the
// cluster-intel project: Postgres (primary metadata), ClickHouse (high-volume
// time-series), and Redis (cache/queues).
//
// All connections are constructed from pkg/config types so that operators
// can point at bundled or external infrastructure without recompiling.
package store
