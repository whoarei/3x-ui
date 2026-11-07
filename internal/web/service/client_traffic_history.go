package service

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TrafficHistoryClient struct {
	Id      int    `json:"id" example:"1"`
	Email   string `json:"email" example:"alice"`
	Comment string `json:"comment" example:"Alice"`
	Deleted bool   `json:"deleted" example:"false"`
}

type TrafficHistoryOptions struct {
	Clients    []TrafficHistoryClient `json:"clients"`
	TimeZone   string                 `json:"timeZone" example:"Asia/Shanghai"`
	StartedAt  int64                  `json:"startedAt" example:"1794096000"`
	LastSample int64                  `json:"lastSample" example:"1794103205"`
}

type TrafficHistoryPoint struct {
	Start           int64 `json:"start" example:"1794096000"`
	End             int64 `json:"end" example:"1794099600"`
	Up              int64 `json:"up" example:"1048576"`
	Down            int64 `json:"down" example:"4194304"`
	CoveredMinutes  int64 `json:"coveredMinutes" example:"60"`
	ExpectedMinutes int64 `json:"expectedMinutes" example:"60"`
}

type TrafficHistoryClientTotal struct {
	TrafficHistoryClient
	Up   int64 `json:"up" example:"1048576"`
	Down int64 `json:"down" example:"4194304"`
}

type TrafficHistoryResponse struct {
	From       int64                       `json:"from" example:"1794096000"`
	To         int64                       `json:"to" example:"1794182400"`
	ClientId   int                         `json:"clientId" example:"0"`
	TimeZone   string                      `json:"timeZone" example:"Asia/Shanghai"`
	Resolution string                      `json:"resolution" example:"hour"`
	Up         int64                       `json:"up" example:"1048576"`
	Down       int64                       `json:"down" example:"4194304"`
	StartedAt  int64                       `json:"startedAt" example:"1794096000"`
	LastSample int64                       `json:"lastSample" example:"1794103205"`
	Points     []TrafficHistoryPoint       `json:"points"`
	Clients    []TrafficHistoryClientTotal `json:"clients"`
}

type ClientTrafficHistoryService struct{}

func recordClientTrafficHistory(tx *gorm.DB, sourceId int, at int64, traffics []*xray.ClientTraffic) error {
	minute := at / 60 * 60
	poll := model.ClientTrafficHistoryPoll{SourceId: sourceId, BucketStart: minute, LastSample: at}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_id"}, {Name: "bucket_start"}},
		DoUpdates: clause.Assignments(map[string]any{"last_sample": gorm.Expr(database.GreatestExpr("client_traffic_history_polls.last_sample", "?"), at)}),
	}).Create(&poll).Error; err != nil {
		return err
	}
	byEmail := make(map[string]*xray.ClientTraffic)
	for _, row := range traffics {
		if row == nil || (row.Up <= 0 && row.Down <= 0) {
			continue
		}
		if prev := byEmail[row.Email]; prev != nil {
			prev.Up = historyAdd(prev.Up, max(0, row.Up))
			prev.Down = historyAdd(prev.Down, max(0, row.Down))
		} else {
			byEmail[row.Email] = &xray.ClientTraffic{Up: max(0, row.Up), Down: max(0, row.Down)}
		}
	}
	if len(byEmail) == 0 {
		return nil
	}
	emails := make([]string, 0, len(byEmail))
	for email := range byEmail {
		emails = append(emails, email)
	}
	for batch := range slices.Chunk(emails, 500) {
		var clients []model.ClientRecord
		if err := tx.Select("id", "email", "comment").Where("email IN ?", batch).Order("id").Find(&clients).Error; err != nil {
			return err
		}
		for _, client := range clients {
			identity := model.ClientTrafficHistoryClient{ClientId: client.Id, Email: client.Email, Comment: client.Comment}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "client_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"email", "comment"}),
			}).Create(&identity).Error; err != nil {
				return err
			}
			delta := byEmail[client.Email]
			bucket := model.ClientTrafficHistoryBucket{ClientId: client.Id, SourceId: sourceId, BucketStart: minute, Up: delta.Up, Down: delta.Down}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "client_id"}, {Name: "source_id"}, {Name: "bucket_start"}},
				DoUpdates: clause.Assignments(map[string]any{
					"up":   gorm.Expr(database.ClampedAddExpr("client_traffic_history_buckets.up"), delta.Up),
					"down": gorm.Expr(database.ClampedAddExpr("client_traffic_history_buckets.down"), delta.Down),
				}),
			}).Create(&bucket).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func historyAdd(a, b int64) int64 {
	if b > math.MaxInt64-a {
		return math.MaxInt64
	}
	return a + b
}

func trafficHistoryBounds(db *gorm.DB) (int64, int64, error) {
	var bounds struct{ StartedAt, LastSample int64 }
	err := db.Model(&model.ClientTrafficHistoryPoll{}).
		Select("COALESCE(MIN(bucket_start), 0) AS started_at, COALESCE(MAX(last_sample), 0) AS last_sample").Scan(&bounds).Error
	return bounds.StartedAt, bounds.LastSample, err
}

func (s *ClientTrafficHistoryService) Options(location *time.Location) (*TrafficHistoryOptions, error) {
	db := database.GetDB()
	result := &TrafficHistoryOptions{TimeZone: location.String(), Clients: []TrafficHistoryClient{}}
	if err := db.Raw(`SELECT id, email, comment, false AS deleted FROM clients
		UNION ALL SELECT h.client_id AS id, h.email, h.comment, true AS deleted
		FROM client_traffic_history_clients h LEFT JOIN clients c ON c.id = h.client_id
		WHERE c.id IS NULL ORDER BY email, id`).Scan(&result.Clients).Error; err != nil {
		return nil, err
	}
	var err error
	result.StartedAt, result.LastSample, err = trafficHistoryBounds(db)
	return result, err
}

func (s *ClientTrafficHistoryService) Query(clientId int, from, to int64, location *time.Location) (*TrafficHistoryResponse, error) {
	if clientId < 0 || from < 0 || to <= from || to > 253402300799 {
		return nil, fmt.Errorf("invalid client or time range")
	}
	from = from / 60 * 60
	to = (to + 59) / 60 * 60
	result := &TrafficHistoryResponse{From: from, To: to, ClientId: clientId, TimeZone: location.String(), Clients: []TrafficHistoryClientTotal{}}
	result.Points, result.Resolution = trafficHistoryRanges(from, to, location)
	db := database.GetDB().Begin()
	if db.Error != nil {
		return nil, db.Error
	}
	defer db.Rollback()
	filter := ""
	args := []any{from, to}
	if clientId > 0 {
		filter = " AND h.client_id = ?"
		args = append(args, clientId)
	}
	if err := db.Raw(`SELECT h.client_id AS id, COALESCE(c.email, i.email) AS email,
		COALESCE(c.comment, i.comment) AS comment, CASE WHEN c.id IS NULL THEN true ELSE false END AS deleted,
		SUM(h.up) AS up, SUM(h.down) AS down
		FROM client_traffic_history_buckets h
		JOIN client_traffic_history_clients i ON i.client_id = h.client_id
		LEFT JOIN clients c ON c.id = h.client_id
		WHERE h.bucket_start >= ? AND h.bucket_start < ?`+filter+`
		GROUP BY h.client_id, c.id, c.email, c.comment, i.email, i.comment ORDER BY SUM(h.up)+SUM(h.down) DESC`, args...).Scan(&result.Clients).Error; err != nil {
		return nil, err
	}
	for _, client := range result.Clients {
		result.Up = historyAdd(result.Up, client.Up)
		result.Down = historyAdd(result.Down, client.Down)
	}
	values := make([]string, len(result.Points))
	rangeArgs := make([]any, 0, len(result.Points)*3+1)
	for i, p := range result.Points {
		values[i] = "(?, ?, ?)"
		rangeArgs = append(rangeArgs, i, p.Start, p.End)
	}
	cte := "WITH ranges (idx, start_at, end_at) AS (VALUES " + strings.Join(values, ",") + ") "
	joinFilter := ""
	byteArgs := append([]any{}, rangeArgs...)
	if clientId > 0 {
		joinFilter = " AND h.client_id = ?"
		byteArgs = append(byteArgs, clientId)
	}
	var bytes []struct {
		Idx      int
		Up, Down int64
	}
	if err := db.Raw(cte+`SELECT r.idx, COALESCE(SUM(h.up),0) AS up, COALESCE(SUM(h.down),0) AS down
		FROM ranges r LEFT JOIN client_traffic_history_buckets h
		ON h.bucket_start >= r.start_at AND h.bucket_start < r.end_at`+joinFilter+`
		GROUP BY r.idx ORDER BY r.idx`, byteArgs...).Scan(&bytes).Error; err != nil {
		return nil, err
	}
	for _, row := range bytes {
		result.Points[row.Idx].Up, result.Points[row.Idx].Down = row.Up, row.Down
	}
	var coverage []struct {
		Idx     int
		Covered int64
	}
	if err := db.Raw(cte+`SELECT r.idx, COUNT(DISTINCT p.bucket_start) AS covered
		FROM ranges r LEFT JOIN client_traffic_history_polls p
		ON p.bucket_start >= r.start_at AND p.bucket_start < r.end_at
		GROUP BY r.idx ORDER BY r.idx`, rangeArgs...).Scan(&coverage).Error; err != nil {
		return nil, err
	}
	for _, row := range coverage {
		result.Points[row.Idx].CoveredMinutes = row.Covered
	}
	var err error
	result.StartedAt, result.LastSample, err = trafficHistoryBounds(db)
	if err != nil {
		return nil, err
	}
	return result, db.Commit().Error
}

func trafficHistoryRanges(from, to int64, location *time.Location) ([]TrafficHistoryPoint, string) {
	span := to - from
	resolution := "hour"
	step := int64(3600)
	if span <= 6*3600 {
		resolution, step = "minute", 60
	} else if span > 14*86400 {
		resolution, step = "day", 86400
	}
	if span > 180*86400 {
		resolution = "week"
	}
	if span > 3*366*86400 {
		resolution = "month"
	}
	monthStep := max(1, int(span/(28*86400*450))+1)
	points := make([]TrafficHistoryPoint, 0, 180)
	for cursor := from; cursor < to; {
		local := time.Unix(cursor, 0).In(location)
		var end int64
		switch resolution {
		case "day", "week":
			day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
			if resolution == "week" {
				day = day.AddDate(0, 0, -(int(day.Weekday())+6)%7)
				end = day.AddDate(0, 0, 7).Unix()
			} else {
				end = day.AddDate(0, 0, 1).Unix()
			}
		case "month":
			end = time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location).AddDate(0, monthStep, 0).Unix()
		default:
			_, offset := local.Zone()
			end = ((cursor+int64(offset))/step+1)*step - int64(offset)
		}
		end = min(end, to)
		points = append(points, TrafficHistoryPoint{Start: cursor, End: end, ExpectedMinutes: (end - cursor) / 60})
		cursor = end
	}
	return points, resolution
}
