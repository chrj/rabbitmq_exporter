package main

import (
	"sync"
	"time"

	"regexp"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/prometheus/client_golang/prometheus"
)

type exporter struct {
	mutex               sync.RWMutex
	queueMetricsGauge   map[string]*prometheus.GaugeVec
	queueMetricsCounter map[string]*prometheus.Desc
	overviewMetrics     map[string]prometheus.Gauge
	upMetric            prometheus.Gauge
	exchangeMetrics     map[string]*prometheus.Desc
	nodeMetricsCounter  map[string]*prometheus.Desc
}

func newExporter() *exporter {
	return &exporter{
		queueMetricsGauge:   queueGaugeVec,
		queueMetricsCounter: queueCounterVec,
		overviewMetrics:     overviewMetricDescription,
		upMetric:            upMetricDescription,
		exchangeMetrics:     exchangeCounterVec,
		nodeMetricsCounter:  nodeCounterVec,
	}
}

func (e *exporter) fetchOverview(ch chan<- prometheus.Metric) error {
	rabbitMqOverviewData, err := getMetricMap(config, "overview")

	if err != nil {
		return err
	}

	log.WithField("overviewData", rabbitMqOverviewData).Debug("Overview data")
	for key, gauge := range e.overviewMetrics {
		if value, ok := rabbitMqOverviewData[key]; ok {
			log.WithFields(log.Fields{"key": key, "value": value}).Debug("Set overview metric for key")
			gauge.Set(value)
		} else {
			log.WithFields(log.Fields{"key": key}).Warn("Overview data not found")
		}
	}
	return nil
}

func (e *exporter) fetchQueue(ch chan<- prometheus.Metric) error {
	rabbitMqQueueData, err := getStatsInfo(config, "queues")

	if err != nil {
		return err
	}

	log.WithField("queueData", rabbitMqQueueData).Debug("Queue data")
	for key, gaugevec := range e.queueMetricsGauge {
		for _, queue := range rabbitMqQueueData {
			if value, ok := queue.metrics[key]; ok {
				if matchInclude, _ := regexp.MatchString(config.IncludeQueues, strings.ToLower(queue.name)); matchInclude {
					if matchSkip, _ := regexp.MatchString(config.SkipQueues, strings.ToLower(queue.name)); !matchSkip {

						log.WithFields(log.Fields{"vhost": queue.vhost, "queue": queue.name, "key": key, "value": value}).Debug("Set queue metric for key")
						gaugevec.WithLabelValues(queue.vhost, queue.name, queue.durable, queue.policy).Set(value)
					}
				}
			}
		}
	}

	for key, countvec := range e.queueMetricsCounter {
		for _, queue := range rabbitMqQueueData {
			if matchInclude, _ := regexp.MatchString(config.IncludeQueues, strings.ToLower(queue.name)); matchInclude {
				if matchSkip, _ := regexp.MatchString(config.SkipQueues, strings.ToLower(queue.name)); !matchSkip {

					if value, ok := queue.metrics[key]; ok {
						log.WithFields(log.Fields{"vhost": queue.vhost, "queue": queue.name, "key": key, "value": value}).Debug("Set queue metric for key")
						ch <- prometheus.MustNewConstMetric(countvec, prometheus.CounterValue, value, queue.vhost, queue.name, queue.durable, queue.policy)
					} else {
						ch <- prometheus.MustNewConstMetric(countvec, prometheus.CounterValue, 0, queue.vhost, queue.name, queue.durable, queue.policy)
					}
				}
			}
		}
	}
	return nil
}

func (e *exporter) fetchExchanges(ch chan<- prometheus.Metric) error {
	exchangeData, err := getStatsInfo(config, "exchanges")

	if err != nil {
		return err
	}

	for key, countvec := range e.exchangeMetrics {
		for _, exchange := range exchangeData {
			if value, ok := exchange.metrics[key]; ok {
				log.WithFields(log.Fields{"vhost": exchange.vhost, "exchange": exchange.name, "key": key, "value": value}).Debug("Set exchange metric for key")
				ch <- prometheus.MustNewConstMetric(countvec, prometheus.CounterValue, value, exchange.vhost, exchange.name)
			}
		}
	}

	return nil
}

func (e *exporter) fetchNodes(ch chan<- prometheus.Metric) error {
	nodeData, err := getStatsInfo(config, "nodes")

	if err != nil {
		return err
	}

	for key, countvec := range e.nodeMetricsCounter {
		for _, node := range nodeData {
			if value, ok := node.metrics[key]; ok {
				log.WithFields(log.Fields{"type": node.vhost, "node": node.name, "key": key, "value": value}).Debug("Set node metric for key")
				ch <- prometheus.MustNewConstMetric(countvec, prometheus.CounterValue, value, node.vhost, node.name)
			}
		}
	}
	return nil

}

func (e *exporter) FetchRabbit(ch chan<- prometheus.Metric) (overviewFetched, queuesFetched bool) {
	start := time.Now()
	allUp := true

	err := e.fetchOverview(ch)
	overviewFetched = err == nil
	if err != nil {
		log.WithError(err).Warn("Overview data was not fetched.")
		allUp = false
	}

	err = e.fetchQueue(ch)
	queuesFetched = err == nil
	if err != nil {
		log.WithError(err).Warn("Queue data was not fetched.")
		allUp = false
	}

	err = e.fetchExchanges(ch)
	if err != nil {
		log.WithError(err).Warn("Exchange data was not fetched.")
		allUp = false
	}

	err = e.fetchNodes(ch)
	if err != nil {
		log.WithError(err).Warn("Node data was not fetched.")
		allUp = false
	}

	if allUp {
		e.upMetric.Set(1)
	} else {
		e.upMetric.Set(0)
	}

	log.WithField("duration", time.Since(start)).Info("Metrics updated")
	return
}

func (e *exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, gauge := range e.overviewMetrics {
		gauge.Describe(ch)
	}

	for _, gaugevec := range e.queueMetricsGauge {
		gaugevec.Describe(ch)
	}
	for _, countervec := range e.queueMetricsCounter {
		ch <- countervec
	}
	for _, exchangeMetric := range e.exchangeMetrics {
		ch <- exchangeMetric
	}

	e.upMetric.Describe(ch)
	BuildInfo.Describe(ch)
}

func (e *exporter) Collect(ch chan<- prometheus.Metric) {
	e.mutex.Lock() // To protect metrics from concurrent collects.
	defer e.mutex.Unlock()

	for _, gaugevec := range e.queueMetricsGauge {
		gaugevec.Reset()
	}

	overviewFetched, queuesFetched := e.FetchRabbit(ch)

	e.upMetric.Collect(ch)
	if overviewFetched {
		for _, gauge := range e.overviewMetrics {
			gauge.Collect(ch)
		}
	}

	if queuesFetched {
		for _, gaugevec := range e.queueMetricsGauge {
			gaugevec.Collect(ch)
		}
	}

	BuildInfo.Collect(ch)
}
