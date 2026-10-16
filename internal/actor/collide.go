package actor

import "sort"

// Obj is a collision object that is not a task (the player's vehicle, the
// player's bullets). Positions are world coordinates.
type Obj struct {
	X, Y, C, E   int16
	Class, Event uint16
}

type item struct {
	x, y, c, e int16
	class      uint16
	t          *Task
	o          *Obj
}

func (it *item) addEvent(v uint16) {
	if it.t != nil {
		it.t.Event |= v
	} else {
		it.o.Event |= v
	}
}

// Collide is the game's collision pass ($15E24/$15E82): every object that is
// not passive (class bit 15) looks for the objects whose x is within its
// half-width $C and whose y is within its half-height $E; each of them gets
// its class ORed into the other's event word. The task's own half sizes
// ($1F4/$1F6) come from the picture it shows (Extents). Events are consumed
// by the tasks in their next step.
func (w *World) Collide(extra ...*Obj) {
	var list []item
	for _, t := range w.tasks {
		if !t.Active || t.done {
			continue
		}
		c, e := 8, 8
		if w.Extents != nil {
			c, e = w.Extents(t.Image)
		}
		list = append(list, item{x: t.ox, y: t.oy, c: int16(c), e: int16(e), class: t.Class, t: t})
	}
	for _, o := range extra {
		list = append(list, item{x: o.X, y: o.Y, c: o.C, e: o.E, class: o.Class, o: o})
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].x < list[j].x })
	for i := range list {
		a := &list[i]
		if a.class&0x8000 != 0 {
			continue // passive
		}
		lo, hi := uint16(a.y-a.e), uint16(a.y+a.e)
		var seen uint16
		for j := range list {
			b := &list[j]
			if i == j || b.x < a.x-a.c || b.x > a.x+a.c {
				continue
			}
			if lo > uint16(b.y) || hi < uint16(b.y) {
				continue
			}
			b.addEvent(a.class)
			seen |= b.class
		}
		a.addEvent(seen)
	}
}
