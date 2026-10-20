package main

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"
)

type Alert struct {
	Time          time.Time `json:"time"`
	Namespace     string    `json:"namespace"`
	PodName       string    `json:"pod_name"`
	ContainerName string    `json:"container_name"`
	Level         string    `json:"level"`
	MatchType     string    `json:"match_type"`
	RuleName      string    `json:"rule_name"`
	Log           string    `json:"log"`
	Raw           string    `json:"raw"`
	RedisID       string    `json:"redis_id"`
}

type CountItem struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type DashboardResponse struct {
	Total      int         `json:"total"`
	Namespaces int         `json:"namespaces"`
	Rules      int         `json:"rules"`
	Trend      []CountItem `json:"trend"`
	ByNamespace []CountItem `json:"by_namespace"`
	ByRule     []CountItem `json:"by_rule"`
	Alerts     []Alert     `json:"alerts"`
}

func (a *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rangeValue := r.URL.Query().Get("range")
	allowedRanges := map[string]string{"1h": "-1h", "6h": "-6h", "24h": "-24h", "7d": "-7d"}
	start, ok := allowedRanges[rangeValue]
	if !ok {
		rangeValue, start = "24h", "-24h"
	}
	limit := 500
	if requested, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && requested > 0 && requested <= 2000 {
		limit = requested
	}
	query := fmt.Sprintf(`from(bucket: %q)
  |> range(start: %s)
  |> filter(fn: (r) => r._measurement == "k8s_log_alerts")
  |> pivot(rowKey: ["_time"], columnKey: ["_field"], valueColumn: "_value")
  |> sort(columns: ["_time"], desc: true)
  |> limit(n: %d)`, a.influxBucket, start, limit)
	result, err := a.influx.QueryAPI(a.influxOrg).Query(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to query alert store: "+err.Error())
		return
	}
	defer result.Close()
	alerts := make([]Alert, 0)
	for result.Next() {
		record := result.Record()
		alerts = append(alerts, Alert{Time: record.Time(), Namespace: stringValue(record.ValueByKey("namespace")),
			PodName: stringValue(record.ValueByKey("pod_name")), ContainerName: stringValue(record.ValueByKey("container_name")),
			Level: stringValue(record.ValueByKey("level")), MatchType: stringValue(record.ValueByKey("match_type")),
			RuleName: stringValue(record.ValueByKey("rule_name")), Log: stringValue(record.ValueByKey("log")),
			Raw: stringValue(record.ValueByKey("raw")), RedisID: stringValue(record.ValueByKey("redis_id"))})
	}
	if result.Err() != nil {
		writeError(w, http.StatusBadGateway, "failed to read alert store: "+result.Err().Error())
		return
	}
	response := summarizeAlerts(alerts, rangeValue)
	writeJSON(w, http.StatusOK, response)
}

func summarizeAlerts(alerts []Alert, rangeValue string) DashboardResponse {
	namespaces, rules, trend := map[string]int{}, map[string]int{}, map[string]int{}
	for _, alert := range alerts {
		namespaces[alert.Namespace]++
		rules[alert.RuleName]++
		bucket := alert.Time.UTC().Format("15:00")
		if rangeValue == "7d" { bucket = alert.Time.UTC().Format("Jan 02") }
		trend[bucket]++
	}
	return DashboardResponse{Total: len(alerts), Namespaces: len(namespaces), Rules: len(rules),
		Trend: sortedCounts(trend, true), ByNamespace: sortedCounts(namespaces, false), ByRule: sortedCounts(rules, false), Alerts: alerts}
}

func sortedCounts(values map[string]int, byName bool) []CountItem {
	items := make([]CountItem, 0, len(values))
	for name, count := range values { items = append(items, CountItem{Name: name, Count: count}) }
	sort.Slice(items, func(i, j int) bool { if byName { return items[i].Name < items[j].Name }; return items[i].Count > items[j].Count })
	return items
}

func stringValue(value interface{}) string {
	if value == nil { return "" }
	return fmt.Sprint(value)
}
