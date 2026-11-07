package service

import (
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"

	"gorm.io/gorm"
)

func TestClientTrafficHistoryKeepsUsageAfterQuotaResetAndDeletion(t *testing.T) {
	db := initTrafficTestDB(t)
	alice := model.ClientRecord{Email: "history-alice", Comment: "Alice"}
	bob := model.ClientRecord{Email: "history-bob"}
	if err := db.Create(&alice).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&bob).Error; err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC).Unix()
	write := func(at int64, rows ...*xray.ClientTraffic) {
		t.Helper()
		if err := db.Transaction(func(tx *gorm.DB) error {
			return recordClientTrafficHistory(tx, 0, at, rows)
		}); err != nil {
			t.Fatal(err)
		}
	}
	write(start+5, &xray.ClientTraffic{Email: alice.Email, Up: 100, Down: 300})
	write(start+30, &xray.ClientTraffic{Email: alice.Email, Up: 50, Down: 70}, &xray.ClientTraffic{Email: bob.Email, Up: 20, Down: 40})
	if err := db.Create(&xray.ClientTraffic{Email: alice.Email, Enable: true, Up: 150, Down: 370}).Error; err != nil {
		t.Fatal(err)
	}
	inbounds := InboundService{}
	if err := inbounds.ResetClientTrafficByEmail(alice.Email); err != nil {
		t.Fatal(err)
	}
	write(start+3605, &xray.ClientTraffic{Email: alice.Email, Up: 10, Down: 30})
	if err := db.Delete(&alice).Error; err != nil {
		t.Fatal(err)
	}
	svc := ClientTrafficHistoryService{}
	got, err := svc.Query(0, start, start+7200, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if got.Up != 180 || got.Down != 440 {
		t.Fatalf("all clients: got %d/%d, want 180/440", got.Up, got.Down)
	}
	got, err = svc.Query(alice.Id, start, start+3600, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if got.Up != 150 || got.Down != 370 {
		t.Fatalf("deleted client in first hour: got %d/%d, want 150/370", got.Up, got.Down)
	}
	options, err := svc.Options(time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range options.Clients {
		if client.Id == alice.Id && client.Deleted && client.Email == alice.Email {
			return
		}
	}
	t.Fatal("deleted customer disappeared from history selector")
}

func TestClientTrafficHistoryRecordsLocalPollAndRemoteDeltasOnce(t *testing.T) {
	db := initTrafficTestDB(t)
	client := model.ClientRecord{Email: "history-nodes"}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&xray.ClientTraffic{Email: client.Email, Enable: true}).Error; err != nil {
		t.Fatal(err)
	}
	svc := InboundService{}
	at := time.Now().Unix()
	if _, _, err := svc.AddTrafficAt(nil, []*xray.ClientTraffic{{Email: client.Email, Up: 10, Down: 20}}, at); err != nil {
		t.Fatal(err)
	}
	createNodeInboundWithClient(t, db, 1, "history-node", 45671, client.Email)
	settings := `{"clients":[{"email":"history-nodes","enable":true}]}`
	syncNodeWithSettings(t, &svc, 1, "history-node", settings, xray.ClientTraffic{Email: client.Email, Up: 1000, Down: 2000, Enable: true})
	syncNodeWithSettings(t, &svc, 1, "history-node", settings, xray.ClientTraffic{Email: client.Email, Up: 1100, Down: 2200, Enable: true})
	syncNodeWithSettings(t, &svc, 1, "history-node", settings, xray.ClientTraffic{Email: client.Email, Up: 1100, Down: 2200, Enable: true})
	history := ClientTrafficHistoryService{}
	got, err := history.Query(client.Id, at/60*60, time.Now().Unix()+60, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if got.Up != 110 || got.Down != 220 {
		t.Fatalf("local + remote deltas got %d/%d, want 110/220 (initial remote totals must not be imported)", got.Up, got.Down)
	}
}

func TestClientTrafficHistoryDailyBucketsFollowDSTAndLimitLongRanges(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 20, 0, 0, 0, 0, location).Unix()
	to := time.Date(2026, 11, 9, 0, 0, 0, 0, location).Unix()
	points, resolution := trafficHistoryRanges(from, to, location)
	if resolution != "day" || len(points) != 20 {
		t.Fatalf("wrong daily range: %s, %d points", resolution, len(points))
	}
	for _, p := range points {
		if time.Unix(p.Start, 0).In(location).Day() == 1 && p.End-p.Start != 25*3600 {
			t.Fatalf("DST day was truncated: %+v", p)
		}
	}
	points, _ = trafficHistoryRanges(0, 253402300740, time.UTC)
	if len(points) > 500 {
		t.Fatalf("long custom range produced %d chart points", len(points))
	}
}

func TestClientTrafficHistoryTransactionRollbackAndMissingMinutes(t *testing.T) {
	db := initTrafficTestDB(t)
	client := model.ClientRecord{Email: "history-rollback"}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC).Unix()
	tx := db.Begin()
	if err := recordClientTrafficHistory(tx, 0, start+5, []*xray.ClientTraffic{{Email: client.Email, Up: 500}}); err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	if err := db.Transaction(func(tx *gorm.DB) error { return recordClientTrafficHistory(tx, 0, start+125, nil) }); err != nil {
		t.Fatal(err)
	}
	svc := ClientTrafficHistoryService{}
	got, err := svc.Query(0, start, start+180, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if got.Up != 0 || got.Down != 0 {
		t.Fatalf("rolled-back bytes leaked: %d/%d", got.Up, got.Down)
	}
	if len(got.Points) != 3 || got.Points[0].CoveredMinutes != 0 || got.Points[2].CoveredMinutes != 1 {
		t.Fatalf("collection gap was lost: %+v", got.Points)
	}
}
