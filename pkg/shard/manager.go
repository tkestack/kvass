package shard

// Manager known how to create or delete Shards
type Manager interface {
	// Shards return current Shards in the cluster
	Shards() ([]*Shard, error)
	// ChangeScale create or delete Shards according to "expReplicate"
	ChangeScale(expReplicate int32) error
}
