package xray

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/structpb"
)

type DomainTrafficRecord struct {
	Email  string `json:"email"`
	Domain string `json:"domain"`
	Minute int64  `json:"minute"`
	Up     int64  `json:"up,string"`
	Down   int64  `json:"down,string"`
}

type DomainTrafficBatch struct {
	Enabled      bool                  `json:"enabled"`
	CoreID       string                `json:"coreId"`
	Sequence     uint64                `json:"sequence,string"`
	StartedAt    int64                 `json:"startedAt"`
	SampledAt    int64                 `json:"sampledAt"`
	Active       int                   `json:"active"`
	Overflow     uint64                `json:"overflow,string"`
	DroppedBytes int64                 `json:"droppedBytes,string"`
	Records      []DomainTrafficRecord `json:"records"`
}

func (x *XrayAPI) GetDomainTraffic(ackCore, ackSequence string) (*DomainTrafficBatch, error) {
	if x.grpcClient == nil {
		return nil, fmt.Errorf("xray API is not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := structpb.NewStruct(map[string]any{"ackCore": ackCore, "ackSequence": ackSequence})
	resp := new(structpb.Struct)
	if err := x.grpcClient.Invoke(ctx, "/xray.app.stats.command.DomainTrafficService/GetBatch", req, resp); err != nil {
		return nil, err
	}
	data, err := json.Marshal(resp.AsMap())
	if err != nil {
		return nil, err
	}
	var batch DomainTrafficBatch
	if err = json.Unmarshal(data, &batch); err != nil {
		return nil, fmt.Errorf("invalid domain batch: %w", err)
	}
	if !batch.Enabled {
		return &batch, nil
	}
	if key, err := hex.DecodeString(batch.CoreID); err != nil || len(key) != 16 || batch.Sequence == 0 || len(batch.Records) > 1000 || batch.SampledAt <= 0 {
		return nil, fmt.Errorf("invalid domain batch metadata")
	}
	for i := range batch.Records {
		r := &batch.Records[i]
		r.Domain = strings.ToLower(strings.TrimSuffix(r.Domain, "."))
		if r.Email == "" || len(r.Email) > 256 || r.Domain == "" || len(r.Domain) > 253 || r.Minute%60 != 0 || r.Minute < batch.StartedAt/60*60 || r.Minute > batch.SampledAt+60 || r.Up < 0 || r.Down < 0 {
			return nil, fmt.Errorf("invalid domain traffic record")
		}
	}
	return &batch, nil
}
