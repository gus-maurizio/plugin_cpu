package main

import (
		"encoding/json"
		"errors"
		"fmt"
		"github.com/shirou/gopsutil/cpu"
		log "github.com/sirupsen/logrus"
    	"time"
)

var PluginEnv		[]cpu.InfoStat
var PluginConfig 	map[string]map[string]map[string]interface{}
var PluginData		map[string]interface{}

var TickCpupercent, CountCpupercent 		int = 1, 0
var NumCpus			int = 1
var MHz 			float64

func PluginMeasure() ([]byte, []byte, float64) {
	// Get measurement of CPU
	PluginData["cpupercent"], _ = cpu.Percent(0, true)
	if TickCpupercent != 0 && CountCpupercent == 0 {
		PluginData["cputimes"],   _ = cpu.Times(true)
		CountCpupercent = (CountCpupercent + 1) % TickCpupercent
	}
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
	for _, cpup := range(PluginData["cpupercent"].([]float64)) {
		if cpup > cpumax {cpumax = cpup}
		if cpup < cpumin {cpumin = cpup}
		cpuavg += cpup
		cpulat += cpup * PluginEnv[0].Mhz / 100.0
	}
	// Prepare the data
	PluginData["cpu"]    		= cpuavg / float64(NumCpus)
	PluginData["cpumax"] 		= cpumax
	PluginData["cpumin"] 		= cpumin
	PluginData["use"]    		= PluginData["cpu"]
	PluginData["latency"]  		= 1e3 / MHz
	PluginData["throughput"]  	= cpulat
	PluginData["throughputmax"] = MHz * float64(NumCpus)
	PluginData["use"]    		= PluginData["cpu"]
	PluginData["saturation"]    = 100.0 * cpumax / PluginConfig["plugin"]["config"]["saturation"].(float64)
	PluginData["errors"]    	= 0.00

	// Prepare a better answer!
	PluginData["measure"] = struct {	
			Cpupercent 		[]float64	`json:"cpupercent"`
			Cpu				float64		`json:"cpu"`
			Use				float64		`json:"use"`
			Latency			float64		`json:"latency"`
			Throughput		float64		`json:"throughput"`
			Throughputmax	float64		`json:"throughputmax"`
	} {		Cpupercent:		PluginData["cpupercent"].([]float64),
			Cpu:			PluginData["cpu"].(float64),
			Use:			PluginData["use"].(float64),
			Throughput:		PluginData["throughput"].(float64),
			Throughputmax:	PluginData["throughputmax"].(float64),
	}

	myMeasure, _		:= json.Marshal(PluginData["measure"])
	myMeasureRaw, _ 	:= json.Marshal(PluginData)
	return myMeasure, myMeasureRaw, float64(time.Now().UnixNano())/1e9
}

func PluginAlert(measure []byte) (string, string, bool, error) {
	// log.WithFields(log.Fields{"MyMeasure": string(MyMeasure[:]), "measure": string(measure[:])}).Info("PluginAlert")
	// var m 			interface{}
	// err := json.Unmarshal(measure, &m)
	// if err != nil { return "unknown", "", true, err }
	alertMsg  := ""
	alertLvl  := ""
	alertFlag := false
	alertErr  := errors.New("")

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

	PluginEnv, _	=  cpu.Info()
	initcpu, _ 		:= cpu.Times(true)
	NumCpus			=  len(initcpu)
	MHz 			=  PluginEnv[0].Mhz

	err := json.Unmarshal([]byte(config), &PluginConfig)
	if err != nil {
		log.WithFields(log.Fields{"config": config}).Error("failed to unmarshal config")
	}

	TickCpupercent	= int(PluginConfig["plugin"]["config"]["cputimes"].(float64))
	log.WithFields(log.Fields{"pluginconfig": PluginConfig, "pluginenv": PluginEnv }).Info("InitPlugin")
}


func main() {
	config  := 	`
				{
					"alert": 
					{
						"cpu":
						{
							"low": 			2,
							"design": 		60.0,
							"engineered":	80.0
						},
				    	"anycpu":
				    	{
				    		"low": 			0,
				    		"design":		75.0,
				    		"engineered":	90.0
				    	}
				    },

					"plugin": 
					{ 
						"config":
						{
							"cputimes":		10,
							"saturation":	75.0
						}
					}
				}
				`

	InitPlugin(config)
	log.WithFields(log.Fields{"PluginConfig": PluginConfig}).Info("InitPlugin")
	tickd := 1* time.Second
	for i := 1; i <= 2; i++ {
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
