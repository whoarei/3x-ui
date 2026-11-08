package service

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/trafficservice"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DomainTrafficService struct{}
type DomainTrafficQuery struct {
	ClientId                int
	From, To                int64
	Service, Domain, Search string
}

type DomainTrafficRank struct {
	Key  string `json:"key" example:"youtube"`
	Up   int64  `json:"up" example:"1048576"`
	Down int64  `json:"down" example:"4194304"`
}

type DomainCollectionView struct {
	Enabled      bool   `json:"enabled" example:"true"`
	CoreId       string `json:"coreId" example:"d350873b45924e919c3650cd5d351e7c"`
	StartedAt    int64  `json:"startedAt" example:"1794096000"`
	LastSample   int64  `json:"lastSample" example:"1794103205"`
	Active       int    `json:"active" example:"20"`
	Overflow     uint64 `json:"overflow" example:"0"`
	DroppedBytes int64  `json:"droppedBytes" example:"0"`
	Error        string `json:"error" example:""`
	RuleServices int    `json:"ruleServices" example:"1403"`
	RuleDomains  int    `json:"ruleDomains" example:"27855"`
	RuleError    string `json:"ruleError" example:""`
	DetailDays   int    `json:"detailDays" example:"30"`
	SummaryDays  int    `json:"summaryDays" example:"365"`
}

type DomainTrafficResponse struct {
	From         int64                 `json:"from" example:"1794096000"`
	To           int64                 `json:"to" example:"1794182400"`
	TimeZone     string                `json:"timeZone" example:"Asia/Shanghai"`
	Resolution   string                `json:"resolution" example:"hour"`
	Archived     bool                  `json:"archived" example:"false"`
	Up           int64                 `json:"up" example:"1048576"`
	Down         int64                 `json:"down" example:"4194304"`
	Points       []TrafficHistoryPoint `json:"points"`
	Services     []DomainTrafficRank   `json:"services"`
	Domains      []DomainTrafficRank   `json:"domains"`
	ServiceCount int64                 `json:"serviceCount" example:"12"`
	DomainCount  int64                 `json:"domainCount" example:"40"`
	Status       DomainCollectionView  `json:"status"`
}

var domainIndexState struct {
	sync.Mutex
	path              string
	checked           time.Time
	modified          int64
	overridesModified int64
	index             *trafficservice.Index
	err               string
}

func domainRetention(name string, fallback int) int {
	n, err := strconv.Atoi(os.Getenv(name))
	if err != nil || n < 1 || n > 3650 {
		return fallback
	}
	return n
}

func domainIndex() (*trafficservice.Index, string) {
	path := os.Getenv("XUI_SERVICE_INDEX")
	if path == "" {
		path = filepath.Join(config.GetBinFolderPath(), "traffic-services.idx")
	}
	domainIndexState.Lock()
	defer domainIndexState.Unlock()
	if path == domainIndexState.path && time.Since(domainIndexState.checked) < time.Minute {
		return domainIndexState.index, domainIndexState.err
	}
	domainIndexState.checked = time.Now()
	info, err := os.Stat(path)
	if err != nil {
		domainIndexState.index = nil
		domainIndexState.err = err.Error()
		domainIndexState.path = path
		return nil, domainIndexState.err
	}
	overridesPath := filepath.Join(config.GetDBFolderPath(), "service-overrides.json")
	var overridesModified int64
	if stat, err := os.Stat(overridesPath); err == nil {
		overridesModified = stat.ModTime().UnixNano()
	}
	if path == domainIndexState.path && info.ModTime().UnixNano() == domainIndexState.modified && overridesModified == domainIndexState.overridesModified && domainIndexState.index != nil {
		return domainIndexState.index, domainIndexState.err
	}
	index, err := trafficservice.Load(path, overridesPath)
	domainIndexState.overridesModified = overridesModified
	domainIndexState.path, domainIndexState.modified = path, info.ModTime().UnixNano()
	domainIndexState.index = index
	domainIndexState.err = ""
	if err != nil {
		domainIndexState.err = err.Error()
	}
	return index, domainIndexState.err
}

func (s *DomainTrafficService) Acknowledgement() (string, string, error) {
	var row model.DomainTrafficCheckpoint
	err := database.GetDB().Table("domain_traffic_checkpoints c").Select("c.*").Joins("JOIN domain_traffic_statuses s ON s.core_id = c.core_id").Where("s.id = 1").Take(&row).Error
	if database.IsNotFound(err) {
		return "", "0", nil
	}
	return row.CoreId, strconv.FormatUint(row.Sequence, 10), err
}

func (s *DomainTrafficService) SetError(message string) error {
	return runSerializedTx(func(tx *gorm.DB) error {
		status := model.DomainTrafficStatus{Id: 1, Error: message}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.Assignments(map[string]any{"error": message, "enabled": false})}).Create(&status).Error
	})
}

func (s *DomainTrafficService) Commit(batch *xray.DomainTrafficBatch, location *time.Location) error {
	if !batch.Enabled {
		return s.SetError("The running kernel has not enabled domain traffic statistics")
	}
	index, _ := domainIndex()
	classified := make([]string, len(batch.Records))
	for i, record := range batch.Records {
		classified[i] = index.Match(record.Domain)
	}
	return runSerializedTx(func(tx *gorm.DB) error {
		var checkpoint model.DomainTrafficCheckpoint
		err := tx.Where("core_id = ?", batch.CoreID).Take(&checkpoint).Error
		if err != nil && !database.IsNotFound(err) {
			return err
		}
		if checkpoint.Sequence >= batch.Sequence {
			return nil
		}
		emailSet := map[string]bool{}
		for _, r := range batch.Records {
			if r.Up < 0 || r.Down < 0 || r.Minute < 0 || r.Minute%60 != 0 {
				return fmt.Errorf("invalid domain traffic")
			}
			emailSet[r.Email] = true
		}
		emails := make([]string, 0, len(emailSet))
		for email := range emailSet {
			emails = append(emails, email)
		}
		clients := map[string]model.ClientRecord{}
		for part := range slices.Chunk(emails, 500) {
			var rows []model.ClientRecord
			if err := tx.Select("id", "email", "comment").Where("email IN ?", part).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				clients[row.Email] = row
				identity := model.ClientTrafficHistoryClient{ClientId: row.Id, Email: row.Email, Comment: row.Comment}
				if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "client_id"}}, DoUpdates: clause.AssignmentColumns([]string{"email", "comment"})}).Create(&identity).Error; err != nil {
					return err
				}
			}
		}
		dimensions := map[string]int64{}
		for i, r := range batch.Records {
			client, exists := clients[r.Email]
			if !exists {
				continue
			}
			service := classified[i]
			identity := r.Domain + "\x00" + service
			domainID := dimensions[identity]
			if domainID == 0 {
				domain := model.TrafficDomain{Host: r.Domain, Service: service}
				if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "host"}, {Name: "service"}}, DoNothing: true}).Create(&domain).Error; err != nil {
					return err
				}
				if domain.Id == 0 {
					if err := tx.Where("host = ? AND service = ?", r.Domain, service).Take(&domain).Error; err != nil {
						return err
					}
				}
				domainID = domain.Id
				dimensions[identity] = domainID
			}
			minute := model.ClientDomainTraffic{ClientId: client.Id, DomainId: domainID, BucketStart: r.Minute, Up: r.Up, Down: r.Down}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "client_id"}, {Name: "domain_id"}, {Name: "bucket_start"}}, DoUpdates: clause.Assignments(map[string]any{
				"up": gorm.Expr(database.ClampedAddExpr("client_domain_traffics.up"), r.Up), "down": gorm.Expr(database.ClampedAddExpr("client_domain_traffics.down"), r.Down),
			})}).Create(&minute).Error; err != nil {
				return err
			}
			at := time.Unix(r.Minute, 0).In(location)
			day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, location).Unix()
			daily := model.ClientServiceDailyTraffic{ClientId: client.Id, Service: service, DayStart: day, Up: r.Up, Down: r.Down}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "client_id"}, {Name: "service"}, {Name: "day_start"}}, DoUpdates: clause.Assignments(map[string]any{
				"up": gorm.Expr(database.ClampedAddExpr("client_service_daily_traffics.up"), r.Up), "down": gorm.Expr(database.ClampedAddExpr("client_service_daily_traffics.down"), r.Down),
			})}).Create(&daily).Error; err != nil {
				return err
			}
		}
		poll := model.DomainTrafficPoll{BucketStart: batch.SampledAt / 60 * 60, LastSample: batch.SampledAt}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "bucket_start"}}, DoUpdates: clause.AssignmentColumns([]string{"last_sample"})}).Create(&poll).Error; err != nil {
			return err
		}
		var old model.DomainTrafficStatus
		if err := tx.Where("id = 1").Take(&old).Error; err != nil && !database.IsNotFound(err) {
			return err
		}
		started := old.StartedAt
		if started == 0 {
			started = batch.StartedAt
		}
		state := model.DomainTrafficStatus{Id: 1, Enabled: true, CoreId: batch.CoreID, StartedAt: started, LastSample: batch.SampledAt, Active: batch.Active, Overflow: batch.Overflow, DroppedBytes: batch.DroppedBytes}
		if err := tx.Save(&state).Error; err != nil {
			return err
		}
		checkpoint = model.DomainTrafficCheckpoint{CoreId: batch.CoreID, Sequence: batch.Sequence, UpdatedAt: batch.SampledAt}
		return tx.Save(&checkpoint).Error
	})
}

func domainStatus(db *gorm.DB) (DomainCollectionView, error) {
	var state model.DomainTrafficStatus
	if err := db.Where("id = 1").Take(&state).Error; err != nil && !database.IsNotFound(err) {
		return DomainCollectionView{}, err
	}
	index, ruleError := domainIndex()
	view := DomainCollectionView{Enabled: state.Enabled, CoreId: state.CoreId, StartedAt: state.StartedAt, LastSample: state.LastSample, Active: state.Active, Overflow: state.Overflow, DroppedBytes: state.DroppedBytes, Error: state.Error, RuleError: ruleError, DetailDays: domainRetention("XUI_DOMAIN_DETAIL_DAYS", 30), SummaryDays: domainRetention("XUI_DOMAIN_SUMMARY_DAYS", 365)}
	if index != nil {
		view.RuleServices = len(index.Header.Services)
		view.RuleDomains = index.Header.Rules
	}
	return view, nil
}

func (s *DomainTrafficService) Query(q DomainTrafficQuery, location *time.Location) (*DomainTrafficResponse, error) {
	if q.ClientId < 0 || q.From < 0 || q.To <= q.From || q.To > 253402300799 || len(q.Search) > 128 || len(q.Domain) > 253 || len(q.Service) > 512 {
		return nil, fmt.Errorf("invalid domain traffic query")
	}
	result := &DomainTrafficResponse{From: q.From / 60 * 60, To: (q.To + 59) / 60 * 60, TimeZone: location.String(), Services: []DomainTrafficRank{}, Domains: []DomainTrafficRank{}}
	state, err := domainStatus(database.GetDB())
	if err != nil {
		return nil, err
	}
	result.Status = state
	cutoff := time.Now().AddDate(0, 0, -state.DetailDays).Unix()
	result.Archived = state.StartedAt > 0 && state.StartedAt < cutoff && q.From < cutoff-120
	if result.Archived {
		if q.Domain != "" {
			return nil, fmt.Errorf("domain detail is retained for %d days; select a recent range", state.DetailDays)
		}
		from := time.Unix(result.From, 0).In(location)
		to := time.Unix(result.To-1, 0).In(location)
		result.From = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, location).Unix()
		result.To = time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, location).AddDate(0, 0, 1).Unix()
	}
	result.Points, result.Resolution = trafficHistoryRanges(result.From, result.To, location)
	if result.Archived && (result.Resolution == "minute" || result.Resolution == "hour") {
		result.Resolution = "day"
		result.Points = nil
		for cursor := result.From; cursor < result.To; {
			local := time.Unix(cursor, 0).In(location)
			end := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).AddDate(0, 0, 1).Unix()
			end = min(end, result.To)
			result.Points = append(result.Points, TrafficHistoryPoint{Start: cursor, End: end, ExpectedMinutes: (end - cursor) / 60})
			cursor = end
		}
	}
	db := database.GetDB().Begin()
	if db.Error != nil {
		return nil, db.Error
	}
	defer db.Rollback()
	table := "client_domain_traffics h JOIN traffic_domains d ON d.id = h.domain_id"
	timeColumn := "h.bucket_start"
	serviceColumn := "d.service"
	if result.Archived {
		table = "client_service_daily_traffics h"
		timeColumn = "h.day_start"
		serviceColumn = "h.service"
	}
	filter := ""
	filterArgs := []any{}
	if q.ClientId > 0 {
		filter += " AND h.client_id = ?"
		filterArgs = append(filterArgs, q.ClientId)
	}
	if q.Service != "" {
		filter += " AND " + serviceColumn + " = ?"
		filterArgs = append(filterArgs, q.Service)
	}
	if q.Domain != "" {
		filter += " AND d.host = ?"
		filterArgs = append(filterArgs, q.Domain)
	}
	if q.Search != "" {
		if result.Archived {
			filter += " AND LOWER(h.service) LIKE ?"
			filterArgs = append(filterArgs, "%"+strings.ToLower(q.Search)+"%")
		} else {
			filter += " AND (LOWER(d.host) LIKE ? OR LOWER(d.service) LIKE ?)"
			pattern := "%" + strings.ToLower(q.Search) + "%"
			filterArgs = append(filterArgs, pattern, pattern)
		}
	}
	where := timeColumn + " >= ? AND " + timeColumn + " < ?" + filter
	args := append([]any{result.From, result.To}, filterArgs...)
	var total struct{ Up, Down int64 }
	if err := db.Raw("SELECT COALESCE(SUM(h.up),0) AS up,COALESCE(SUM(h.down),0) AS down FROM "+table+" WHERE "+where, args...).Scan(&total).Error; err != nil {
		return nil, err
	}
	result.Up, result.Down = total.Up, total.Down
	if err := db.Raw("SELECT "+serviceColumn+" AS key,SUM(h.up) AS up,SUM(h.down) AS down FROM "+table+" WHERE "+where+" GROUP BY "+serviceColumn+" ORDER BY SUM(h.up)+SUM(h.down) DESC LIMIT 100", args...).Scan(&result.Services).Error; err != nil {
		return nil, err
	}
	if err := db.Raw("SELECT COUNT(DISTINCT "+serviceColumn+") FROM "+table+" WHERE "+where, args...).Scan(&result.ServiceCount).Error; err != nil {
		return nil, err
	}
	if !result.Archived {
		if err := db.Raw("SELECT d.host AS key,SUM(h.up) AS up,SUM(h.down) AS down FROM "+table+" WHERE "+where+" GROUP BY d.host ORDER BY SUM(h.up)+SUM(h.down) DESC LIMIT 100", args...).Scan(&result.Domains).Error; err != nil {
			return nil, err
		}
		if err := db.Raw("SELECT COUNT(DISTINCT d.host) FROM "+table+" WHERE "+where, args...).Scan(&result.DomainCount).Error; err != nil {
			return nil, err
		}
	}
	values := make([]string, len(result.Points))
	rangeArgs := make([]any, 0, len(values)*3)
	for i, p := range result.Points {
		values[i] = "(?, ?, ?)"
		rangeArgs = append(rangeArgs, i, p.Start, p.End)
	}
	cte := "WITH ranges (idx,start_at,end_at) AS (VALUES " + strings.Join(values, ",") + ") "
	var rows []struct {
		Idx      int
		Up, Down int64
	}
	joined := append(append([]any{}, rangeArgs...), args...)
	if err := db.Raw(cte+"SELECT r.idx,COALESCE(SUM(t.up),0) AS up,COALESCE(SUM(t.down),0) AS down FROM ranges r LEFT JOIN (SELECT "+timeColumn+" AS ts,h.up,h.down FROM "+table+" WHERE "+where+") t ON t.ts >= r.start_at AND t.ts < r.end_at GROUP BY r.idx ORDER BY r.idx", joined...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result.Points[row.Idx].Up, result.Points[row.Idx].Down = row.Up, row.Down
	}
	var coverage []struct {
		Idx     int
		Minutes int64
	}
	if err := db.Raw(cte+"SELECT r.idx,COUNT(p.bucket_start) AS minutes FROM ranges r LEFT JOIN domain_traffic_polls p ON p.bucket_start >= r.start_at AND p.bucket_start < r.end_at GROUP BY r.idx", rangeArgs...).Scan(&coverage).Error; err != nil {
		return nil, err
	}
	for _, row := range coverage {
		result.Points[row.Idx].CoveredMinutes = row.Minutes
	}
	return result, db.Commit().Error
}

func (s *DomainTrafficService) Cleanup() error {
	minutes := time.Now().AddDate(0, 0, -domainRetention("XUI_DOMAIN_DETAIL_DAYS", 30)).Unix()
	days := time.Now().AddDate(0, 0, -domainRetention("XUI_DOMAIN_SUMMARY_DAYS", 365)).Unix()
	return runSerializedTx(func(tx *gorm.DB) error {
		for _, item := range []struct {
			Table, Column string
			Before        int64
		}{{"client_domain_traffics", "bucket_start", minutes}, {"client_service_daily_traffics", "day_start", days}} {
			if err := tx.Exec("DELETE FROM "+item.Table+" WHERE id IN (SELECT id FROM "+item.Table+" WHERE "+item.Column+" < ? ORDER BY id LIMIT 10000)", item.Before).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("bucket_start < ?", days).Delete(&model.DomainTrafficPoll{}).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM traffic_domains WHERE id IN (SELECT d.id FROM traffic_domains d WHERE NOT EXISTS (SELECT 1 FROM client_domain_traffics h WHERE h.domain_id=d.id) LIMIT 1000)").Error; err != nil {
			return err
		}
		return tx.Where("updated_at < ?", days).Delete(&model.DomainTrafficCheckpoint{}).Error
	})
}
