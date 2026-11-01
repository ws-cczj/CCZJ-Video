package library

import (
	CCZJ "cczjVideo/app/cczj"
	"context"
	"encoding/json"
	"strconv"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"cczjVideo/app/provider"
)

const novelSubscriptionURL = "https://raw.githubusercontent.com/aoaostar/legado/master/sources/b778fe6b.json"
const novelSubscriptionSnapshot = CCZJ.SettingNovelSubscriptionRules

func (s *Service) refreshNovelSubscription(ctx context.Context) (resultErr error) {
	defer func() {
		if ctx.Err() != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			if err := db.SetSettings(cleanup, map[string]string{CCZJ.SettingNovelSubscriptionNext: "0"}); err != nil {
				resultErr = err
			}
			return
		}
		if resultErr != nil && ctx.Err() == nil {
			if err := db.SetSettings(ctx, map[string]string{CCZJ.SettingNovelSubscriptionError: string(apperror.CodeOf(resultErr))}); err != nil {
				resultErr = err
			}
		}
	}()
	raw, err := db.GetSetting(CCZJ.SettingNovelSubscriptionNext)
	if err != nil {
		return err
	}
	next, _ := strconv.ParseInt(raw, 10, 64)
	if next > time.Now().Unix() {
		return nil
	}
	if err := db.SetSettings(ctx, map[string]string{CCZJ.SettingNovelSubscriptionAttempted: strconv.FormatInt(time.Now().Unix(), 10), CCZJ.SettingNovelSubscriptionNext: strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)}); err != nil {
		return err
	}
	response, err := s.fetcher.ScopeMode("novel-subscription", "novel").FetchSourceList(ctx, novelSubscriptionURL)
	if err != nil {
		return err
	}
	candidates, err := ParseBatchSources([]byte(response.Text), "novel", novelSubscriptionURL)
	if err != nil {
		return err
	}
	registry, err := s.registryFor(model.SourceKindNovel)
	if err != nil {
		return err
	}
	installed, err := db.GetSourcesByKindContext(ctx, model.SourceKindNovel)
	if err != nil {
		return err
	}
	byID := map[string]*BatchSource{}
	for _, row := range registry {
		byID[row.ID] = row
	}
	remote := map[string][]*BatchSource{}
	for _, row := range candidates {
		if row.CanRead && row.doc.Adapter == "" {
			remote[row.URL] = append(remote[row.URL], row)
		}
	}
	type snapshot struct {
		Name     string          `json:"name"`
		URL      string          `json:"apiUrl"`
		Kind     string          `json:"kind"`
		Origin   string          `json:"origin"`
		Project  string          `json:"project"`
		Document json.RawMessage `json:"document"`
	}
	snapshots := []snapshot{}
	updates := []db.ReadingRuleUpdate{}
	rejected := false
	for _, source := range installed {
		prior := byID[sourceQualityID("novel", source.ApiUrl, source.StrategyConfig)]
		if prior == nil || prior.doc == nil || prior.doc.Adapter != "" {
			continue
		}
		current := prior
		matches := remote[source.ApiUrl]
		if len(matches) == 1 {
			current = matches[0]
		}
		if current.document != source.StrategyConfig {
			call, cancel := context.WithTimeout(ctx, 12*time.Second)
			candidate := &resolvedSource{source: source, document: current.doc, pipeline: provider.NewPipeline(current.doc, s.fetcher.ScopeMode(source.SourceKey+":"+current.ID, "novel"), s.limits)}
			outcome := s.testResolvedSource(call, source.SourceKey, candidate)
			cancel()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !outcome.OK {
				current = prior
				rejected = true
			}
		}
		if current.document != source.StrategyConfig {
			updates = append(updates, db.ReadingRuleUpdate{SourceKey: source.SourceKey, Previous: source.StrategyConfig, Document: current.document})
		}
		snapshots = append(snapshots, snapshot{Name: source.Name, URL: source.ApiUrl, Kind: "novel", Origin: current.Origin, Project: current.Project, Document: json.RawMessage(current.document)})
	}
	encoded, err := json.Marshal(snapshots)
	if err != nil {
		return err
	}
	now := time.Now()
	code, interval := "", 24*time.Hour
	if rejected {
		code, interval = "RULE_VALIDATION_FAILED", time.Hour
	}
	if err := db.ApplyReadingRuleUpdates(ctx, updates, map[string]string{novelSubscriptionSnapshot: string(encoded), CCZJ.SettingNovelSubscriptionError: code, CCZJ.SettingNovelSubscriptionChecked: strconv.FormatInt(now.Unix(), 10), CCZJ.SettingNovelSubscriptionNext: strconv.FormatInt(now.Add(interval).Unix(), 10)}); err != nil {
		return err
	}
	s.registryMu.Lock()
	delete(s.registry, "novel")
	s.registryMu.Unlock()
	return nil
}
