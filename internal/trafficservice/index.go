package trafficservice

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/xray/geodata"
	"golang.org/x/net/publicsuffix"
)

const rowSize = 36

type Extra struct {
	Service uint32 `json:"service"`
	Kind    string `json:"kind"`
	Value   string `json:"value"`
}
type Header struct {
	SourceHash  string   `json:"sourceHash"`
	GeneratedAt int64    `json:"generatedAt"`
	Services    []string `json:"services"`
	Rules       int      `json:"rules"`
	Extra       []Extra  `json:"extra"`
}
type compiledExtra struct {
	Extra
	expression *regexp.Regexp
}
type Index struct {
	Header    Header
	rows      []byte
	extra     []compiledExtra
	mu        sync.RWMutex
	cache     map[string]string
	overrides map[string]string
}

func serviceCategory(code string, count int) bool {
	if code == "" || code == "cn" || code == "!cn" || count <= 0 || count > 10000 {
		return false
	}
	for _, prefix := range []string{"category-", "geolocation-", "tld-", "geoip-", "geosite-", "antipiracy", "private", "ads", "malware", "phishing", "cryptominers", "win-spy", "oisd", "easylist", "easyprivacy"} {
		if strings.HasPrefix(code, prefix) {
			return false
		}
	}
	return true
}

func Generate(input, output string) (Header, error) {
	store := geodata.NewStore(filepath.Dir(input))
	page, err := store.Categories(filepath.Base(input), "", 0, 0)
	if err != nil {
		return Header{}, err
	}
	sort.Slice(page.Items, func(i, j int) bool {
		a, b := page.Items[i], page.Items[j]
		if a.Entries != b.Entries {
			return a.Entries < b.Entries
		}
		return a.Code < b.Code
	})
	header := Header{GeneratedAt: time.Now().Unix(), Services: []string{}, Extra: []Extra{}}
	rules := make(map[[32]byte]uint32)
	for _, category := range page.Items {
		if !serviceCategory(category.Code, category.Entries) {
			continue
		}
		id := uint32(len(header.Services))
		header.Services = append(header.Services, category.Code)
		for offset := 0; offset < category.Entries; offset += geodata.MaxPageSize {
			entries, err := store.Entries(filepath.Base(input), category.Code, "", offset, geodata.MaxPageSize)
			if err != nil {
				return Header{}, err
			}
			for _, entry := range entries.Items {
				value := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(entry.Value)), ".")
				if entry.Kind == "regexp" {
					value = strings.TrimSpace(entry.Value)
				}
				switch entry.Kind {
				case "full", "domain":
					if len(value) > 253 || !strings.Contains(value, ".") {
						continue
					}
					hash := sha256.Sum256([]byte(entry.Kind + "\x00" + value))
					if _, exists := rules[hash]; !exists {
						rules[hash] = id
					}
				case "keyword", "regexp":
					if len(header.Extra) < 1024 && len(value) <= 512 {
						header.Extra = append(header.Extra, Extra{Service: id, Kind: entry.Kind, Value: value})
					}
				}
			}
		}
	}
	source, err := os.ReadFile(input)
	if err != nil {
		return Header{}, err
	}
	hash := sha256.Sum256(source)
	header.SourceHash = hex.EncodeToString(hash[:])
	source = nil
	header.Rules = len(rules)
	keys := make([][32]byte, 0, len(rules))
	for k := range rules {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare(keys[i][:], keys[j][:]) < 0 })
	meta, err := json.Marshal(header)
	if err != nil {
		return Header{}, err
	}
	data := make([]byte, 8+len(meta)+len(keys)*rowSize)
	copy(data, "XDS1")
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(meta)))
	copy(data[8:], meta)
	for i, key := range keys {
		row := data[8+len(meta)+i*rowSize:]
		copy(row, key[:])
		binary.LittleEndian.PutUint32(row[32:36], rules[key])
	}
	if err := os.WriteFile(output, data, 0644); err != nil {
		return Header{}, err
	}
	return header, nil
}

func Load(path, overridePath string) (*Index, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > 32<<20 {
		return nil, fmt.Errorf("service index exceeds 32 MiB")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 8 || string(data[:4]) != "XDS1" {
		return nil, fmt.Errorf("invalid service index")
	}
	n := int(binary.LittleEndian.Uint32(data[4:8]))
	if n > 1<<20 || n > len(data)-8 || (len(data)-8-n)%rowSize != 0 {
		return nil, fmt.Errorf("invalid service index length")
	}
	index := &Index{rows: data[8+n:], cache: make(map[string]string), overrides: make(map[string]string)}
	if err := json.Unmarshal(data[8:8+n], &index.Header); err != nil {
		return nil, err
	}
	if index.Header.Rules != len(index.rows)/rowSize {
		return nil, fmt.Errorf("service index count mismatch")
	}
	for p := 0; p < len(index.rows)/rowSize; p++ {
		row := index.rows[p*rowSize : (p+1)*rowSize]
		if int(binary.LittleEndian.Uint32(row[32:])) >= len(index.Header.Services) {
			return nil, fmt.Errorf("invalid service row")
		}
		if p > 0 && bytes.Compare(index.rows[(p-1)*rowSize:(p-1)*rowSize+32], row[:32]) >= 0 {
			return nil, fmt.Errorf("service index is not sorted")
		}
	}
	for _, extra := range index.Header.Extra {
		if int(extra.Service) >= len(index.Header.Services) {
			return nil, fmt.Errorf("invalid service ID")
		}
		item := compiledExtra{Extra: extra}
		if extra.Kind == "regexp" {
			item.expression, _ = regexp.Compile(extra.Value)
			if item.expression == nil {
				continue
			}
		}
		index.extra = append(index.extra, item)
	}
	if overridePath != "" {
		if stat, err := os.Stat(overridePath); err == nil && stat.Size() <= 1<<20 {
			data, err := os.ReadFile(overridePath)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(data, &index.overrides); err != nil {
				return nil, fmt.Errorf("invalid service overrides: %w", err)
			}
			normalized := make(map[string]string, len(index.overrides))
			for host, service := range index.overrides {
				host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
				if host == "" || len(host) > 253 || service == "" || len(service) > 128 {
					return nil, fmt.Errorf("invalid service override")
				}
				normalized[host] = service
			}
			index.overrides = normalized
		}
	}
	return index, nil
}

func (i *Index) lookup(kind, domain string) string {
	hash := sha256.Sum256([]byte(kind + "\x00" + domain))
	n := len(i.rows) / rowSize
	p := sort.Search(n, func(p int) bool { return bytes.Compare(i.rows[p*rowSize:p*rowSize+32], hash[:]) >= 0 })
	if p < n && bytes.Equal(i.rows[p*rowSize:p*rowSize+32], hash[:]) {
		id := binary.LittleEndian.Uint32(i.rows[p*rowSize+32 : p*rowSize+36])
		if int(id) < len(i.Header.Services) {
			return i.Header.Services[id]
		}
	}
	return ""
}

func Fallback(domain string) string {
	if domain == "" || domain == "__unknown__" {
		return "unknown"
	}
	root, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		root = domain
	}
	return "domain:" + root
}

func (i *Index) Match(domain string) string {
	if i == nil {
		return Fallback(domain)
	}
	i.mu.RLock()
	cached, exists := i.cache[domain]
	i.mu.RUnlock()
	if exists {
		return cached
	}
	result := ""
	for current := domain; current != ""; {
		if custom := i.overrides[current]; custom != "" {
			result = "custom:" + custom
			break
		}
		p := strings.IndexByte(current, '.')
		if p < 0 {
			break
		}
		current = current[p+1:]
	}
	if result != "" {
		i.mu.Lock()
		if len(i.cache) >= 4096 {
			clear(i.cache)
		}
		i.cache[domain] = result
		i.mu.Unlock()
		return result
	}
	for current := domain; strings.Contains(current, "."); {
		if current == domain {
			result = i.lookup("full", current)
		}
		if result == "" {
			result = i.lookup("domain", current)
		}
		if result != "" {
			break
		}
		current = current[strings.IndexByte(current, '.')+1:]
	}
	if result == "" {
		for _, extra := range i.extra {
			if (extra.Kind == "keyword" && strings.Contains(domain, extra.Value)) || (extra.expression != nil && extra.expression.MatchString(domain)) {
				result = i.Header.Services[extra.Service]
				break
			}
		}
	}
	if result == "" {
		result = Fallback(domain)
	}
	i.mu.Lock()
	if len(i.cache) >= 4096 {
		clear(i.cache)
	}
	i.cache[domain] = result
	i.mu.Unlock()
	return result
}
