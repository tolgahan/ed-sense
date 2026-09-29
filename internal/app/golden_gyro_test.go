package app

import (
	"math"
	"time"

	"github.com/tolgahan/ed-sense/internal/config"
	"github.com/tolgahan/ed-sense/internal/dualsense"
	"github.com/tolgahan/ed-sense/internal/elite"
)

// turn: a report of the controller turning at yaw, pitch and roll deg/s,
// lying flat.
func turn(yaw, pitch, roll float64, touch bool) *dualsense.State {
	raw := func(deg float64) int16 { return int16(math.Round(deg * 16.384)) }
	return &dualsense.State{Gyro: [3]int16{raw(pitch), raw(yaw), raw(roll)}, Accel: [3]int16{0, 8192, 0}, Touch: touch}
}

func ownGyro(c *config.Config) { c.GyroBy = config.GyroByEDSense }
func dsxGyro(c *config.Config) { c.GyroBy = config.GyroByDSX }

// flying: DSX answers, the pad and its audio are open, the ship in space.
func flying(p *player) {
	p.dsx.Answering, p.pad.Open, p.audio.Up = true, true, true
	p.writeStatus(elite.Status{Flags: ship})
}

// EDSense's gyro: scripted sessions with gyro_by "edsense". The mouse lines
// add up the movement of each tick.
var gyroScripts = []script{
	{"gyro-own-flight", ownGyro, func(p *player) {
		flying(p)
		p.run(2500 * time.Millisecond)
		p.pad.Stream = turn(20, 0, 0, false)
		p.note("turning left at 20 deg/s")
		p.run(time.Second)
		p.pad.Stream = turn(0, -20, 0, false)
		p.note("tilting down at 20 deg/s")
		p.run(time.Second)
		p.pad.Stream = turn(0, 0, 20, false)
		p.note("rolling at 20 deg/s")
		p.run(500 * time.Millisecond)
		p.pad.Stream = nil
		p.note("still")
		p.run(4 * time.Second)
		p.stop()
	}},
	{"gyro-own-menus", ownGyro, func(p *player) {
		flying(p)
		p.pad.Stream = turn(20, 0, 0, false)
		p.run(time.Second)
		p.writeStatus(elite.Status{Flags: ship, GuiFocus: 6})
		p.note("galaxy map")
		p.run(2 * time.Second)
		p.writeStatus(elite.Status{Flags: ship, GuiFocus: 9})
		p.note("FSS")
		p.run(time.Second)
		p.event(`{"event":"Music","MusicTrack":"MainMenu"}`)
		p.note("main menu")
		p.run(time.Second)
		p.event(`{"event":"LoadGame","Ship":"python"}`)
		p.writeStatus(elite.Status{Flags: ship})
		p.note("back in flight")
		p.run(time.Second)
		p.stop()
	}},
	{"gyro-own-touch", ownGyro, func(p *player) {
		flying(p)
		p.run(time.Second)
		p.pad.Stream = turn(20, 0, 0, true)
		p.note("turning with a finger on the touchpad")
		p.run(time.Second)
		p.pad.Stream = turn(20, 0, 0, false)
		p.note("finger lifted")
		p.run(time.Second)
		p.stop()
	}},
	{"gyro-own-front", ownGyro, func(p *player) {
		flying(p)
		p.pad.Stream = turn(20, 0, 0, false)
		p.run(time.Second)
		p.front = false
		p.note("alt-tab")
		p.run(time.Second)
		p.front = true
		p.note("back in Elite")
		p.run(time.Second)
		p.stop()
	}},
	{"gyro-own-pause", ownGyro, func(p *player) {
		flying(p)
		p.pad.Stream = turn(20, 0, 0, false)
		p.run(time.Second)
		p.s.SetPaused(true)
		p.note("paused")
		p.run(time.Second)
		p.s.SetPaused(false)
		p.note("resumed")
		p.run(time.Second)
		p.stop()
	}},
	{"gyro-own-dsx-drops", ownGyro, func(p *player) {
		flying(p)
		p.pad.Stream = turn(20, 0, 0, false)
		p.run(time.Second)
		p.dsx.Answering = false
		p.note("DSX stops answering")
		p.run(time.Second)
		p.dsx.Answering = true
		p.note("DSX answers again")
		p.run(time.Second)
		p.stop()
	}},
	{"gyro-own-pad-lost", ownGyro, func(p *player) {
		flying(p)
		p.pad.Stream = turn(20, 0, 0, false)
		p.run(time.Second)
		p.pad.Open = false
		p.note("the virtual pad is gone")
		p.run(time.Second)
		p.pad.Open = true
		p.note("the virtual pad is back")
		p.run(time.Second)
		p.stop()
	}},
	{"gyro-own-nodata", ownGyro, func(p *player) {
		flying(p)
		p.pad.Stream = &dualsense.State{}
		p.note("reports without motion")
		p.run(4 * time.Second)
		p.writeStatus(elite.Status{Flags: ship, GuiFocus: 6})
		p.note("galaxy map")
		p.run(time.Second)
		p.stop()
	}},
	{"gyro-own-elevated", ownGyro, func(p *player) {
		p.blocked = true
		flying(p)
		p.pad.Stream = turn(20, 0, 0, false)
		p.run(2 * time.Second)
		p.stop()
	}},
	{"gyro-own-switch", dsxGyro, func(p *player) {
		flying(p)
		p.pad.Stream = turn(20, 0, 0, false)
		p.run(time.Second)
		p.config(ownGyro)
		p.run(3 * time.Second)
		p.config(func(c *config.Config) { c.GyroAim = false })
		p.run(3 * time.Second)
		p.writeStatus(elite.Status{Flags: ship, GuiFocus: 6})
		p.config(func(c *config.Config) { c.GyroAim, c.GyroBy = true, config.GyroByDSX })
		p.run(3 * time.Second)
		p.stop()
	}},
	{"gyro-own-calibrate", ownGyro, func(p *player) {
		flying(p)
		p.run(500 * time.Millisecond)
		p.s.CalibrateGyro()
		p.note("Calibrate gyro, the controller still")
		p.run(3 * time.Second)
		p.s.CalibrateGyro()
		p.pad.Stream = turn(20, 0, 0, false)
		p.note("Calibrate gyro, the controller turning")
		p.run(3 * time.Second)
		p.stop()
	}},
	{"gyro-own-slow", ownGyro, func(p *player) {
		flying(p)
		p.run(2500 * time.Millisecond)
		p.pad.Stream = turn(0.8, 0, 0, false)
		p.note("turning at 0.8 deg/s")
		p.run(3 * time.Second)
		p.config(func(c *config.Config) { c.GyroLowSpeed = config.GyroLowExact })
		p.run(4 * time.Second)
		p.stop()
	}},
	{"gyro-own-profile", ownGyro, func(p *player) {
		p.setup.Known = true // the DSX profile has the gyro on a stick
		flying(p)
		p.pad.Stream = turn(20, 0, 0, false)
		p.run(2 * time.Second)
		p.setup.Mouse = true
		p.note("the DSX profile's gyro is motion to mouse")
		p.run(time.Second)
		p.stop()
	}},
	{"gyro-own-no-controller", ownGyro, func(p *player) {
		p.dsx.Slots = nil
		flying(p)
		p.pad.Stream = turn(20, 0, 0, false)
		p.run(time.Second)
		p.dsx.Slots = []int{0}
		p.note("DSX lists the controller")
		p.run(time.Second)
		p.dsx.Slots = nil
		p.note("DSX lists none")
		p.run(time.Second)
		p.stop()
	}},
	{"gyro-dsx-calibrate", dsxGyro, func(p *player) {
		flying(p)
		p.writeStatus(elite.Status{Flags: ship, GuiFocus: 6})
		p.run(time.Second)
		p.s.CalibrateGyro()
		p.note("Calibrate gyro in the galaxy map")
		p.run(3 * time.Second)
		p.pad.Stream = &dualsense.State{}
		p.s.CalibrateGyro()
		p.note("Calibrate gyro, no motion data")
		p.run(4 * time.Second)
		p.s.SetPaused(true)
		p.pad.Stream = nil
		p.s.CalibrateGyro()
		p.note("Calibrate gyro, paused")
		p.run(3 * time.Second)
		p.s.running = false
		p.s.CalibrateGyro()
		p.note("Calibrate gyro, Elite closed")
		p.run(500 * time.Millisecond)
		p.stop()
	}},
	{"gyro-own-elite-closes", ownGyro, func(p *player) {
		flying(p)
		p.pad.Stream = turn(20, 0, 0, false)
		p.run(time.Second)
		p.s.running = false
		p.note("Elite closed")
		p.run(time.Second)
		p.s.running = true
		p.event(`{"event":"LoadGame","Ship":"python"}`)
		p.note("Elite back")
		p.run(time.Second)
		p.stop()
	}},
}
