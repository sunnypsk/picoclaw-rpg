package cron

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/fileutil"
	"github.com/sipeed/picoclaw/pkg/routing"
)

type RepairOptions struct {
	ID                 string
	ExpectedHash       string
	Apply              bool
	Timezone           string
	MinVerifiedSources *int
}
type RepairReport struct {
	ID      string `json:"id"`
	Before  string `json:"before"`
	After   string `json:"after"`
	Changed bool   `json:"changed"`
}

// Repair operates on raw JSON to preserve unknown fields and unselected jobs.
// The gateway must be stopped by the operator before apply.
func Repair(path string, options RepairOptions, resolver *routing.RouteResolver) ([]RepairReport, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	if options.Apply && (options.ID == "" || options.ExpectedHash != hash) {
		return nil, hash, fmt.Errorf("apply requires job ID and matching file SHA-256")
	}
	if options.Timezone != "" {
		if _, err := time.LoadLocation(options.Timezone); err != nil {
			return nil, hash, err
		}
	}
	if options.MinVerifiedSources != nil && *options.MinVerifiedSources < 0 {
		return nil, hash, fmt.Errorf("source minimum must be nonnegative")
	}
	var root map[string]json.RawMessage
	if err = json.Unmarshal(data, &root); err != nil {
		return nil, hash, err
	}
	var jobs []json.RawMessage
	if err = json.Unmarshal(root["jobs"], &jobs); err != nil {
		return nil, hash, err
	}
	reports := []RepairReport{}
	changed := false
	for i, raw := range jobs {
		var job CronJob
		if err = json.Unmarshal(raw, &job); err != nil {
			return nil, hash, err
		}
		if options.ID != "" && job.ID != options.ID {
			continue
		}
		input := routing.ScheduledSourceInput(job.Payload.Channel, job.Payload.To, job.Payload.SessionKey, job.Payload.SourcePeer)
		peer := input.Peer
		if peer == nil || input.Channel == "" {
			return nil, hash, fmt.Errorf("job %s missing destination", job.ID)
		}
		route := resolver.ResolveRoute(input)
		if routing.IsConversationSource(job.Payload.SessionKey) {
			parsed := routing.ParseAgentSessionKey(job.Payload.SessionKey)
			if parsed.AgentID == route.AgentID && strings.EqualFold(job.Payload.SessionKey, route.SessionKey) {
				route.SessionKey = job.Payload.SessionKey
			}
		}

		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		var payload map[string]json.RawMessage
		_ = json.Unmarshal(fields["payload"], &payload)
		payload["session_key"], _ = json.Marshal(route.SessionKey)
		payload["source_peer"], _ = json.Marshal(peer)
		if options.MinVerifiedSources != nil {
			payload["min_verified_sources"], _ = json.Marshal(*options.MinVerifiedSources)
		}
		fields["payload"], _ = json.Marshal(payload)
		if options.Timezone != "" {
			var schedule map[string]json.RawMessage
			_ = json.Unmarshal(fields["schedule"], &schedule)
			schedule["tz"], _ = json.Marshal(options.Timezone)
			fields["schedule"], _ = json.Marshal(schedule)
		}
		updated, _ := json.Marshal(fields)
		// Compare canonical raw maps so repeated applies do not rewrite data.
		var original map[string]json.RawMessage
		_ = json.Unmarshal(raw, &original)
		originalBytes, _ := json.Marshal(original)
		isChanged := !bytes.Equal(updated, originalBytes)
		reports = append(reports, RepairReport{job.ID, job.Payload.SessionKey, route.SessionKey, isChanged})
		if isChanged {
			jobs[i] = updated
			changed = true
		}
	}
	if options.ID != "" && len(reports) != 1 {
		return nil, hash, fmt.Errorf("expected exactly one matching job")
	}
	if !options.Apply || !changed {
		return reports, hash, nil
	}
	backup := path + ".backup-" + hash
	if old, err := os.ReadFile(backup); err == nil {
		if !bytes.Equal(old, data) {
			return nil, hash, fmt.Errorf("backup already exists with different data")
		}
	} else if !os.IsNotExist(err) {
		return nil, hash, err
	} else if err = fileutil.WriteFileAtomic(backup, data, 0600); err != nil {
		return nil, hash, err
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return nil, hash, err
	}
	if !bytes.Equal(current, data) {
		return nil, hash, fmt.Errorf("cron file changed during repair")
	}
	root["jobs"], _ = json.Marshal(jobs)
	updated, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, hash, err
	}
	return reports, hash, fileutil.WriteFileAtomic(path, updated, 0600)
}
