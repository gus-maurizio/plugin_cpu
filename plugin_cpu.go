package main

import (
		"encoding/json"
		"errors"
		"fmt"
		"github.com/shirou/gopsutil/cpu"
		log "github.com/sirupsen/logrus"
		"github.com/prometheus/client_golang/prometheus"
		"github.com/prometheus/client_golang/prometheus/promhttp"
		"net/http"
    	"time"
)

var PluginEnv		[]cpu.InfoStat
var PluginConfig 	map[string]map[string]map[string]interface{}
var PluginData		map[string]interface{}

var NumCpus			int = 1
var MHz 			float64


//	Define the metrics we wish to expose
var cpuIndicator = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "sreagent_cpu_metrics",
		Help: "CPU Utilization Saturation Errors Throughput Latency",
	}, []string{"use"} )

var cpuPercent = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "sreagent_cpu_percent",
		Help: "Host CPU utilization per core 0/00",
	}, []string{"cpu"} )

var cpuMhz = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "sreagent_cpu_mhz",
		Help: "Host CPU utilization per core MHz",
	}, []string{"cpu"} )


func PluginMeasure() ([]byte, []byte, float64) {
	// Get measurement of CPU
	PluginData["cpupercent"], _ 	= cpu.Percent(0, true)
	// Make it understandable
	// Apply USE methodology for CPU
	// U: 	Usage (usually throughput/latency indicators)
	//		In this case we define as CPU average utilization 0-100
	//		Latency is the MHz of each CPU weighted by usage
	// S:	Saturation (measured relevant to Design point)
	// E:	Errors (not applicable for CPU)
	cpuavg := 0.0
	cpumax := 0.0
	cpumin := 100.0
	cpulat := 0.0
	for cpuidx, cpup := range(PluginData["cpupercent"].([]float64)) {
		if cpup > cpumax {cpumax = cpup}
		if cpup < cpumin {cpumin = cpup}
		cpuavg += cpup
		cpulat += cpup * MHz / 100.0
		// Update metrics related to the plugin
		cpuPercent.With(prometheus.Labels{"cpu": fmt.Sprintf("cpu%d",cpuidx)}).Set(cpup)
		cpuMhz.With(prometheus.Labels{"cpu": fmt.Sprintf("cpu%d",cpuidx)}).Set(cpup * MHz / 100.0)
	}
	// Prepare the data
	PluginData["cpu"]    		= cpuavg / float64(NumCpus)
	PluginData["cpumax"] 		= cpumax
	PluginData["cpumin"] 		= cpumin
	PluginData["use"]    		= PluginData["cpu"]
	PluginData["latency"]  		= 1e3 / MHz
	PluginData["throughput"]  	= cpulat
	PluginData["throughputmax"] = MHz * float64(NumCpus)
	PluginData["saturation"]    = PluginData["cpu"]
	PluginData["errors"]    	= 0.00

	// Update metrics related to the plugin
	cpuIndicator.With(prometheus.Labels{"use":  "utilization"}).Set(PluginData["use"].(float64))
	cpuIndicator.With(prometheus.Labels{"use":  "saturation"}).Set(PluginData["saturation"].(float64))
	cpuIndicator.With(prometheus.Labels{"use":  "throughput"}).Set(PluginData["throughput"].(float64))
	cpuIndicator.With(prometheus.Labels{"use":  "errors"}).Set(PluginData["errors"].(float64))


	// // Prepare a better answer!
	// PluginData["measure"] = struct {	
	// 		Cpupercent 		[]float64	`json:"cpupercent"`
	// 		Cpu				float64		`json:"cpu"`
	// 		Use				float64		`json:"use"`
	// 		Latency			float64		`json:"latency"`
	// 		Throughput		float64		`json:"throughput"`
	// 		Throughputmax	float64		`json:"throughputmax"`
	// } {		Cpupercent:		PluginData["cpupercent"].([]float64),
	// 		Cpu:			PluginData["cpu"].(float64),
	// 		Use:			PluginData["use"].(float64),
	// 		Throughput:		PluginData["throughput"].(float64),
	// 		Throughputmax:	PluginData["throughputmax"].(float64),
	// }

	//myMeasure, _		:= json.Marshal(PluginData["measure"])
	myMeasure, _ 	:= json.Marshal(PluginData)
	return myMeasure, []byte(""), float64(time.Now().UnixNano())/1e9
}

func PluginAlert(measure []byte) (string, string, bool, error) {
	// log.WithFields(log.Fields{"MyMeasure": string(MyMeasure[:]), "measure": string(measure[:])}).Info("PluginAlert")
	// var m 			interface{}
	// err := json.Unmarshal(measure, &m)
	// if err != nil { return "unknown", "", true, err }
	alertMsg  := ""
	alertLvl  := ""
	alertFlag := false
	alertErr  := errors.New("no error")

	// Check that the CPU overall value is within range
	switch {
		case PluginData["cpu"].(float64) < PluginConfig["alert"]["cpu"]["low"].(float64):
			alertLvl  = "warn"
			alertMsg  += "Overall CPU below low design point "
			alertFlag = true
			alertErr  = errors.New("low cpu")
		case PluginData["cpu"].(float64) > PluginConfig["alert"]["cpu"]["engineered"].(float64):
			alertLvl  = "fatal"
			alertMsg  += "Overall CPU above engineered point "
			alertFlag = true
			alertErr  = errors.New("excessive cpu")
			// return now, looks bad
			return alertMsg, alertLvl, alertFlag, alertErr
		case PluginData["cpu"].(float64) > PluginConfig["alert"]["cpu"]["design"].(float64):
			alertLvl  = "warn"
			alertMsg  += "Overall CPU above design point "
			alertFlag = true
			alertErr  = errors.New("moderately high cpu")
	}
	// Check each CPU for potential issues with usage
	for cpuid, eachcpu := range(PluginData["cpupercent"].([]float64)) {
		switch {
			case eachcpu < PluginConfig["alert"]["anycpu"]["low"].(float64):
				alertLvl  = "warn"
				alertMsg  += fmt.Sprintf("CPU %d below low design point: %f ",cpuid,eachcpu)
				alertFlag = true
				alertErr  = errors.New("low cpu")
			case eachcpu > PluginConfig["alert"]["anycpu"]["engineered"].(float64):
				alertLvl  = "fatal"
				alertMsg  += fmt.Sprintf("CPU %d above engineered point: %f ",cpuid,eachcpu)
				alertFlag = true
				alertErr  = errors.New("excessive cpu")
				// return now, looks bad
				return alertMsg, alertLvl, alertFlag, alertErr
			case eachcpu > PluginConfig["alert"]["anycpu"]["design"].(float64):
				alertLvl  = "warn"
				alertMsg  += fmt.Sprintf("CPU %d above design point: %f ",cpuid,eachcpu)
				alertFlag = true
				alertErr  = errors.New("moderately high cpu")
		}	
	}
	return alertMsg, alertLvl, alertFlag, alertErr
}


func InitPlugin(config string) () {
	if PluginData  		== nil {
		PluginData 		=  make(map[string]interface{},20)
	}
	if PluginConfig  	== nil {
		PluginConfig 	=  make(map[string]map[string]map[string]interface{},20)
	}

	cpu.Percent(0,true)	// needs initialization before next call to avoid a 0 answer
	
	// Register metrics with prometheus
	prometheus.MustRegister(cpuIndicator)
	prometheus.MustRegister(cpuPercent)
	prometheus.MustRegister(cpuMhz)

	PluginEnv, _	=  cpu.Info()
	initcpu, _ 		:= cpu.Times(true)
	NumCpus			=  len(initcpu)
	MHz 			=  PluginEnv[0].Mhz

	err := json.Unmarshal([]byte(config), &PluginConfig)
	if err != nil {
		log.WithFields(log.Fields{"config": config}).Error("failed to unmarshal config")
	}

	log.WithFields(log.Fields{"pluginconfig": PluginConfig, "pluginenv": PluginEnv }).Info("InitPlugin")
}


func main() {
	config  := 	`
				{
					"alert": 
					{
						"/":
						{
							"low": 			2,
							"design": 		60.0,
							"engineered":	80.0
						},
						"/Volumes/TOSHIBA-001":
						{
							"low": 			22,
							"design": 		40.0,
							"engineered":	75.0
						}
				    }
				}
				`

	//--------------------------------------------------------------------------//
	// time to start a prometheus metrics server
	// and export any metrics on the /metrics endpoint.
	http.Handle("/metrics", promhttp.Handler())
	go func() {
		http.ListenAndServe(":8999", nil)
	}()
	//--------------------------------------------------------------------------//

	InitPlugin(config)
	log.WithFields(log.Fields{"PluginConfig": PluginConfig}).Info("InitPlugin")
	tickd := 1* time.Second
	for i := 1; i <= 10; i++ {
		tick := time.Now().UnixNano()
		measure, measureraw, measuretimestamp := PluginMeasure()
		alertmsg, alertlvl, isAlert, err := PluginAlert(measure)
		fmt.Printf("Iteration #%d tick %d \n", i, tick)
		log.WithFields(log.Fields{"timestamp": measuretimestamp, 
					  "measure": string(measure[:]),
					  "measureraw": string(measureraw[:]),
					  "PluginData": PluginData,
					  "alertMsg": alertmsg,
					  "alertLvl": alertlvl,
					  "isAlert":  isAlert,
					  "AlertErr":      err,
		}).Info("Tick")
		time.Sleep(tickd)
	}
}
