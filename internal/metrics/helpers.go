package metrics

import "github.com/prometheus/client_golang/prometheus"

// NewCounter 向指定注册表注册一个计数器。
func NewCounter(reg *prometheus.Registry, name, help string) prometheus.Counter {
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: name, Help: help})
	reg.MustRegister(c)
	return c
}

// NewLabeledCounter 注册带标签的计数器。
func NewLabeledCounter(reg *prometheus.Registry, name, help string, labels ...string) *prometheus.CounterVec {
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels)
	reg.MustRegister(c)
	return c
}

// NewGauge 向指定注册表注册一个 gauge。
func NewGauge(reg *prometheus.Registry, name, help string) prometheus.Gauge {
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: name, Help: help})
	reg.MustRegister(g)
	return g
}

// NewLabeledGauge 注册带标签的 gauge。
func NewLabeledGauge(reg *prometheus.Registry, name, help string, labels ...string) *prometheus.GaugeVec {
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help}, labels)
	reg.MustRegister(g)
	return g
}

// NewHistogram 注册直方图。
func NewHistogram(reg *prometheus.Registry, name, help string, buckets []float64) prometheus.Histogram {
	h := prometheus.NewHistogram(prometheus.HistogramOpts{Name: name, Help: help, Buckets: buckets})
	reg.MustRegister(h)
	return h
}
