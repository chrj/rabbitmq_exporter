package main

import (
	"encoding/json"
	"net/http"

	log "github.com/Sirupsen/logrus"
)

func getMetrics(config rabbitExporterConfig, endpoint string) *json.Decoder {
	client := &http.Client{}
	req, err := http.NewRequest("GET", config.RABBIT_URL+"/api/"+endpoint, nil)
	req.SetBasicAuth(config.RABBIT_USER, config.RABBIT_PASSWORD)

	resp, err := client.Do(req)

	if err != nil || resp == nil || resp.StatusCode != 200 {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		log.WithFields(log.Fields{"error": err, "host": config.RABBIT_URL, "statusCode": status}).Error("Error while retrieving data from rabbitHost")
		return nil
	}
	return json.NewDecoder(resp.Body)
}

func getQueueMap(config rabbitExporterConfig) map[string]metricMap {
	metric := getMetrics(config, "queues")
	qm := MakeQueueMap(metric)
	return qm
}

func getOverviewMap(config rabbitExporterConfig) metricMap {
	metric := getMetrics(config, "overview")
	overview := MakeMap(metric)
	return overview
}
