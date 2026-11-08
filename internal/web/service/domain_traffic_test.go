package service

import (
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestDomainTrafficAcknowledgesOnlyCommittedBatchesAndKeepsResetHistory(t *testing.T) {
	db := initTrafficTestDB(t)
	client := model.ClientRecord{Email: "domain-alice"}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	minute := time.Now().Unix() / 60 * 60
	batch := &xray.DomainTrafficBatch{Enabled: true, CoreID: strings.Repeat("a", 32), Sequence: 1, StartedAt: minute, SampledAt: minute + 5, Records: []xray.DomainTrafficRecord{{Email: client.Email, Domain: "www.example.com", Minute: minute, Up: 123, Down: 456}}}
	svc := DomainTrafficService{}
	if err := svc.Commit(batch, time.UTC); err != nil {
		t.Fatal(err)
	}
	if err := svc.Commit(batch, time.UTC); err != nil {
		t.Fatal(err)
	}
	ack, sequence, err := svc.Acknowledgement()
	if err != nil || ack != batch.CoreID || sequence != "1" {
		t.Fatalf("wrong durable acknowledgement: %s/%s, %v", ack, sequence, err)
	}
	inbounds := InboundService{}
	if err := inbounds.ResetClientTrafficByEmail(client.Email); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Query(DomainTrafficQuery{ClientId: client.Id, From: minute, To: minute + 60}, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if result.Up != 123 || result.Down != 456 || len(result.Domains) != 1 || result.Domains[0].Key != "www.example.com" {
		t.Fatalf("retry duplicated bytes or reset cleared history: %+v", result)
	}
	if len(result.Services) != 1 || result.Services[0].Key != "domain:example.com" {
		t.Fatalf("unclassified domains disappeared: %+v", result.Services)
	}
}

func TestDomainArchiveUsesWholeDaysAndRejectsExpiredHostDetail(t *testing.T) {
	db := initTrafficTestDB(t)
	day := time.Now().UTC().AddDate(0, 0, -45).Truncate(24 * time.Hour).Unix()
	status := model.DomainTrafficStatus{Id: 1, StartedAt: day - 86400, Enabled: true, LastSample: time.Now().Unix()}
	if err := db.Create(&status).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientServiceDailyTraffic{ClientId: 1, Service: "google", DayStart: day, Up: 123, Down: 456}).Error; err != nil {
		t.Fatal(err)
	}
	svc := DomainTrafficService{}
	got, err := svc.Query(DomainTrafficQuery{From: day + 3600, To: day + 7200}, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Archived || got.Resolution != "day" || len(got.Points) != 1 || got.From != day || got.To != day+86400 || got.Up != 123 || got.Down != 456 {
		t.Fatalf("archive pretended hourly detail was available: %+v", got)
	}
	_, err = svc.Query(DomainTrafficQuery{From: day, To: day + 86400, Domain: "google.com"}, time.UTC)
	if err == nil || !strings.Contains(err.Error(), "retained") {
		t.Fatalf("expired domain detail should be rejected, got %v", err)
	}
}
