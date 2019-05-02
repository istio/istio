// Copyright 2018 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// +build !windows

package sysfs

import (
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/prometheus/procfs/internal/util"
)

// PowerSupply contains info from files in /sys/class/power_supply for a single power supply.
type PowerSupply struct {
	Name                     string // Power Supply Name
	Authentic                *int64 `fileName:"authentic"`                   // /sys/class/power_suppy/<Name>/authentic
	Calibrate                *int64 `fileName:"calibrate"`                   // /sys/class/power_suppy/<Name>/calibrate
	Capacity                 *int64 `fileName:"capacity"`                    // /sys/class/power_suppy/<Name>/capacity
	CapacityAlertMax         *int64 `fileName:"capacity_alert_max"`          // /sys/class/power_suppy/<Name>/capacity_alert_max
	CapacityAlertMin         *int64 `fileName:"capacity_alert_min"`          // /sys/class/power_suppy/<Name>/capacity_alert_min
	CapacityLevel            string `fileName:"capacity_level"`              // /sys/class/power_suppy/<Name>/capacity_level
	ChargeAvg                *int64 `fileName:"charge_avg"`                  // /sys/class/power_suppy/<Name>/charge_avg
	ChargeControlLimit       *int64 `fileName:"charge_control_limit"`        // /sys/class/power_suppy/<Name>/charge_control_limit
	ChargeControlLimitMax    *int64 `fileName:"charge_control_limit_max"`    // /sys/class/power_suppy/<Name>/charge_control_limit_max
	ChargeCounter            *int64 `fileName:"charge_counter"`              // /sys/class/power_suppy/<Name>/charge_counter
	ChargeEmpty              *int64 `fileName:"charge_empty"`                // /sys/class/power_suppy/<Name>/charge_empty
	ChargeEmptyDesign        *int64 `fileName:"charge_empty_design"`         // /sys/class/power_suppy/<Name>/charge_empty_design
	ChargeFull               *int64 `fileName:"charge_full"`                 // /sys/class/power_suppy/<Name>/charge_full
	ChargeFullDesign         *int64 `fileName:"charge_full_design"`          // /sys/class/power_suppy/<Name>/charge_full_design
	ChargeNow                *int64 `fileName:"charge_now"`                  // /sys/class/power_suppy/<Name>/charge_now
	ChargeTermCurrent        *int64 `fileName:"charge_term_current"`         // /sys/class/power_suppy/<Name>/charge_term_current
	ChargeType               string `fileName:"charge_type"`                 // /sys/class/power_supply/<Name>/charge_type
	ConstantChargeCurrent    *int64 `fileName:"constant_charge_current"`     // /sys/class/power_suppy/<Name>/constant_charge_current
	ConstantChargeCurrentMax *int64 `fileName:"constant_charge_current_max"` // /sys/class/power_suppy/<Name>/constant_charge_current_max
	ConstantChargeVoltage    *int64 `fileName:"constant_charge_voltage"`     // /sys/class/power_suppy/<Name>/constant_charge_voltage
	ConstantChargeVoltageMax *int64 `fileName:"constant_charge_voltage_max"` // /sys/class/power_suppy/<Name>/constant_charge_voltage_max
	CurrentAvg               *int64 `fileName:"current_avg"`                 // /sys/class/power_suppy/<Name>/current_avg
	CurrentBoot              *int64 `fileName:"current_boot"`                // /sys/class/power_suppy/<Name>/current_boot
	CurrentMax               *int64 `fileName:"current_max"`                 // /sys/class/power_suppy/<Name>/current_max
	CurrentNow               *int64 `fileName:"current_now"`                 // /sys/class/power_suppy/<Name>/current_now
	CycleCount               *int64 `fileName:"cycle_count"`                 // /sys/class/power_suppy/<Name>/cycle_count
	EnergyAvg                *int64 `fileName:"energy_avg"`                  // /sys/class/power_supply/<Name>/energy_avg
	EnergyEmpty              *int64 `fileName:"energy_empty"`                // /sys/class/power_suppy/<Name>/energy_empty
	EnergyEmptyDesign        *int64 `fileName:"energy_empty_design"`         // /sys/class/power_suppy/<Name>/energy_empty_design
	EnergyFull               *int64 `fileName:"energy_full"`                 // /sys/class/power_suppy/<Name>/energy_full
	EnergyFullDesign         *int64 `fileName:"energy_full_design"`          // /sys/class/power_suppy/<Name>/energy_full_design
	EnergyNow                *int64 `fileName:"energy_now"`                  // /sys/class/power_supply/<Name>/energy_now
	Health                   string `fileName:"health"`                      // /sys/class/power_suppy/<Name>/health
	InputCurrentLimit        *int64 `fileName:"input_current_limit"`         // /sys/class/power_suppy/<Name>/input_current_limit
	Manufacturer             string `fileName:"manufacturer"`                // /sys/class/power_suppy/<Name>/manufacturer
	ModelName                string `fileName:"model_name"`                  // /sys/class/power_suppy/<Name>/model_name
	Online                   *int64 `fileName:"online"`                      // /sys/class/power_suppy/<Name>/online
	PowerAvg                 *int64 `fileName:"power_avg"`                   // /sys/class/power_suppy/<Name>/power_avg
	PowerNow                 *int64 `fileName:"power_now"`                   // /sys/class/power_suppy/<Name>/power_now
	PrechargeCurrent         *int64 `fileName:"precharge_current"`           // /sys/class/power_suppy/<Name>/precharge_current
	Present                  *int64 `fileName:"present"`                     // /sys/class/power_suppy/<Name>/present
	Scope                    string `fileName:"scope"`                       // /sys/class/power_suppy/<Name>/scope
	SerialNumber             string `fileName:"serial_number"`               // /sys/class/power_suppy/<Name>/serial_number
	Status                   string `fileName:"status"`                      // /sys/class/power_supply/<Name>/status
	Technology               string `fileName:"technology"`                  // /sys/class/power_suppy/<Name>/technology
	Temp                     *int64 `fileName:"temp"`                        // /sys/class/power_suppy/<Name>/temp
	TempAlertMax             *int64 `fileName:"temp_alert_max"`              // /sys/class/power_suppy/<Name>/temp_alert_max
	TempAlertMin             *int64 `fileName:"temp_alert_min"`              // /sys/class/power_suppy/<Name>/temp_alert_min
	TempAmbient              *int64 `fileName:"temp_ambient"`                // /sys/class/power_suppy/<Name>/temp_ambient
	TempAmbientMax           *int64 `fileName:"temp_ambient_max"`            // /sys/class/power_suppy/<Name>/temp_ambient_max
	TempAmbientMin           *int64 `fileName:"temp_ambient_min"`            // /sys/class/power_suppy/<Name>/temp_ambient_min
	TempMax                  *int64 `fileName:"temp_max"`                    // /sys/class/power_suppy/<Name>/temp_max
	TempMin                  *int64 `fileName:"temp_min"`                    // /sys/class/power_suppy/<Name>/temp_min
	TimeToEmptyAvg           *int64 `fileName:"time_to_empty_avg"`           // /sys/class/power_suppy/<Name>/time_to_empty_avg
	TimeToEmptyNow           *int64 `fileName:"time_to_empty_now"`           // /sys/class/power_suppy/<Name>/time_to_empty_now
	TimeToFullAvg            *int64 `fileName:"time_to_full_avg"`            // /sys/class/power_suppy/<Name>/time_to_full_avg
	TimeToFullNow            *int64 `fileName:"time_to_full_now"`            // /sys/class/power_suppy/<Name>/time_to_full_now
	Type                     string `fileName:"type"`                        // /sys/class/power_supply/<Name>/type
	UsbType                  string `fileName:"usb_type"`                    // /sys/class/power_supply/<Name>/usb_type
	VoltageAvg               *int64 `fileName:"voltage_avg"`                 // /sys/class/power_supply/<Name>/voltage_avg
	VoltageBoot              *int64 `fileName:"voltage_boot"`                // /sys/class/power_suppy/<Name>/voltage_boot
	VoltageMax               *int64 `fileName:"voltage_max"`                 // /sys/class/power_suppy/<Name>/voltage_max
	VoltageMaxDesign         *int64 `fileName:"voltage_max_design"`          // /sys/class/power_suppy/<Name>/voltage_max_design
	VoltageMin               *int64 `fileName:"voltage_min"`                 // /sys/class/power_suppy/<Name>/voltage_min
	VoltageMinDesign         *int64 `fileName:"voltage_min_design"`          // /sys/class/power_suppy/<Name>/voltage_min_design
	VoltageNow               *int64 `fileName:"voltage_now"`                 // /sys/class/power_supply/<Name>/voltage_now
	VoltageOCV               *int64 `fileName:"voltage_ocv"`                 // /sys/class/power_suppy/<Name>/voltage_ocv
}

// PowerSupplyClass is a collection of every power supply in /sys/class/power_supply/.
// The map keys are the names of the power supplies.
type PowerSupplyClass map[string]PowerSupply

// NewPowerSupplyClass returns info for all power supplies read from /sys/class/power_supply/.
func NewPowerSupplyClass() (PowerSupplyClass, error) {
	fs, err := NewFS(DefaultMountPoint)
	if err != nil {
		return nil, err
	}

	return fs.NewPowerSupplyClass()
}

// NewPowerSupplyClass returns info for all power supplies read from /sys/class/power_supply/.
func (fs FS) NewPowerSupplyClass() (PowerSupplyClass, error) {
	path := fs.Path("class/power_supply")

	powerSupplyDirs, err := ioutil.ReadDir(path)
	if err != nil {
		return PowerSupplyClass{}, fmt.Errorf("cannot access %s dir %s", path, err)
	}

	powerSupplyClass := PowerSupplyClass{}
	for _, powerSupplyDir := range powerSupplyDirs {
		powerSupply, err := powerSupplyClass.parsePowerSupply(path + "/" + powerSupplyDir.Name())
		if err != nil {
			return nil, err
		}
		powerSupply.Name = powerSupplyDir.Name()
		powerSupplyClass[powerSupplyDir.Name()] = *powerSupply
	}
	return powerSupplyClass, nil
}

func (psc PowerSupplyClass) parsePowerSupply(powerSupplyPath string) (*PowerSupply, error) {
	powerSupply := PowerSupply{}
	powerSupplyElem := reflect.ValueOf(&powerSupply).Elem()
	powerSupplyType := reflect.TypeOf(powerSupply)

	//start from 1 - skip the Name field
	for i := 1; i < powerSupplyElem.NumField(); i++ {
		fieldType := powerSupplyType.Field(i)
		fieldValue := powerSupplyElem.Field(i)

		if fieldType.Tag.Get("fileName") == "" {
			panic(fmt.Errorf("field %s does not have a filename tag", fieldType.Name))
		}

		value, err := util.SysReadFile(powerSupplyPath + "/" + fieldType.Tag.Get("fileName"))

		if err != nil {
			if os.IsNotExist(err) || err.Error() == "operation not supported" || err.Error() == "invalid argument" {
				continue
			}
			return nil, fmt.Errorf("could not access file %s: %s", fieldType.Tag.Get("fileName"), err)
		}

		switch fieldValue.Kind() {
		case reflect.String:
			fieldValue.SetString(value)
		case reflect.Ptr:
			var int64ptr *int64
			switch fieldValue.Type() {
			case reflect.TypeOf(int64ptr):
				var intValue int64
				if strings.HasPrefix(value, "0x") {
					intValue, err = strconv.ParseInt(value[2:], 16, 64)
					if err != nil {
						return nil, fmt.Errorf("expected hex value for %s, got: %s", fieldType.Name, value)
					}
				} else {
					intValue, err = strconv.ParseInt(value, 10, 64)
					if err != nil {
						return nil, fmt.Errorf("expected Uint64 value for %s, got: %s", fieldType.Name, value)
					}
				}
				fieldValue.Set(reflect.ValueOf(&intValue))
			default:
				return nil, fmt.Errorf("unhandled pointer type %q", fieldValue.Type())
			}
		default:
			return nil, fmt.Errorf("unhandled type %q", fieldValue.Kind())
		}
	}

	return &powerSupply, nil
}
