package model

type TrafficDomain struct {
	Id      int64  `gorm:"primaryKey"`
	Host    string `gorm:"uniqueIndex:idx_traffic_domain_identity,priority:1"`
	Service string `gorm:"uniqueIndex:idx_traffic_domain_identity,priority:2;index:idx_traffic_domain_service"`
}

type ClientDomainTraffic struct {
	Id          int64 `gorm:"primaryKey"`
	ClientId    int   `gorm:"uniqueIndex:idx_client_domain_minute,priority:1;index:idx_domain_client_time,priority:1"`
	DomainId    int64 `gorm:"uniqueIndex:idx_client_domain_minute,priority:2;index:idx_domain_traffic_domain"`
	BucketStart int64 `gorm:"uniqueIndex:idx_client_domain_minute,priority:3;index:idx_domain_traffic_time;index:idx_domain_client_time,priority:2"`
	Up          int64
	Down        int64
}

type ClientServiceDailyTraffic struct {
	Id       int64  `gorm:"primaryKey"`
	ClientId int    `gorm:"uniqueIndex:idx_client_service_day,priority:1"`
	Service  string `gorm:"uniqueIndex:idx_client_service_day,priority:2"`
	DayStart int64  `gorm:"uniqueIndex:idx_client_service_day,priority:3;index:idx_service_daily_time"`
	Up       int64
	Down     int64
}

type DomainTrafficCheckpoint struct {
	CoreId    string `gorm:"primaryKey"`
	Sequence  uint64
	UpdatedAt int64
}

type DomainTrafficStatus struct {
	Id           int `gorm:"primaryKey;autoIncrement:false"`
	Enabled      bool
	CoreId       string
	StartedAt    int64
	LastSample   int64
	Active       int
	Overflow     uint64
	DroppedBytes int64
	Error        string
}

type DomainTrafficPoll struct {
	BucketStart int64 `gorm:"primaryKey;autoIncrement:false"`
	LastSample  int64
}
