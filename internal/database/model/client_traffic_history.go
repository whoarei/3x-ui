package model

// History identities deliberately have no cascading foreign key to clients.
// Deleting an account must not remove its past usage.
type ClientTrafficHistoryClient struct {
	ClientId int `gorm:"primaryKey;autoIncrement:false"`
	Email    string
	Comment  string
}

type ClientTrafficHistoryBucket struct {
	ClientId    int   `gorm:"primaryKey;autoIncrement:false;index:idx_traffic_history_client_time,priority:1"`
	SourceId    int   `gorm:"primaryKey;autoIncrement:false"`
	BucketStart int64 `gorm:"primaryKey;autoIncrement:false;index:idx_traffic_history_time;index:idx_traffic_history_client_time,priority:2"`
	Up          int64
	Down        int64
}

type ClientTrafficHistoryPoll struct {
	SourceId    int   `gorm:"primaryKey;autoIncrement:false"`
	BucketStart int64 `gorm:"primaryKey;autoIncrement:false;index:idx_traffic_history_poll_time"`
	LastSample  int64
}
