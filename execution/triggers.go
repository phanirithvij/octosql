package execution

import (
	"fmt"
	"time"

	"github.com/google/btree"
)

type Trigger interface {
	EndOfStreamReached()
	WatermarkReceived(watermark time.Time)
	KeyReceived(key GroupKey)
	Poll() []GroupKey
	// ExpireKeysBeforeTime(time time.Time)
}

type CountingTrigger struct {
	triggerAfter uint

	counts             *btree.BTree
	endOfStreamReached bool
	toTrigger          []GroupKey
}

func NewCountingTriggerPrototype(triggerAfter uint) func() Trigger {
	return func() Trigger {
		return &CountingTrigger{
			triggerAfter:       triggerAfter,
			counts:             btree.New(DefaultBTreeDegree),
			endOfStreamReached: false,
			toTrigger:          []GroupKey{},
		}
	}
}

type countingTriggerItem struct {
	GroupKey
	Count uint
}

func (c *CountingTrigger) EndOfStreamReached() {
	c.endOfStreamReached = true
}

func (c *CountingTrigger) WatermarkReceived(watermark time.Time) {}

func (c *CountingTrigger) KeyReceived(key GroupKey) {
	item := c.counts.Get(key)
	var itemTyped *countingTriggerItem

	if item == nil {
		itemTyped = &countingTriggerItem{GroupKey: key}
		c.counts.ReplaceOrInsert(itemTyped)
	} else {
		var ok bool
		itemTyped, ok = item.(*countingTriggerItem)
		if !ok {
			panic(fmt.Sprintf("invalid received item: %v", item))
		}
	}
	itemTyped.Count++
	if itemTyped.Count == c.triggerAfter {
		c.toTrigger = append(c.toTrigger, itemTyped.GroupKey)
		c.counts.Delete(itemTyped)
	}
}

// The returned slice will be made invalid after following operations on the trigger.
func (c *CountingTrigger) Poll() []GroupKey {
	output := c.toTrigger
	c.toTrigger = c.toTrigger[:0]
	if c.endOfStreamReached {
		c.counts.Ascend(func(item btree.Item) bool {
			itemTyped, ok := item.(*countingTriggerItem)
			if !ok {
				panic(fmt.Sprintf("invalid received item: %v", item))
			}

			output = append(output, itemTyped.GroupKey)

			return true
		})
	}
	return output
}

type WatermarkTrigger struct {
	timeKeys           *btree.BTree
	endOfStreamReached bool
	watermark          time.Time

	outputKeysSlice []GroupKey
}

func NewWatermarkTriggerPrototype() func() Trigger {
	return func() Trigger {
		return &WatermarkTrigger{
			timeKeys:           btree.New(DefaultBTreeDegree),
			endOfStreamReached: false,
			watermark:          time.Time{},
			outputKeysSlice:    make([]GroupKey, 0),
		}
	}
}

func (c *WatermarkTrigger) EndOfStreamReached() {
	c.endOfStreamReached = true
}

func (c *WatermarkTrigger) WatermarkReceived(watermark time.Time) {
	c.watermark = watermark
}

// TODO: Event time has to be the first element of the key.
func (c *WatermarkTrigger) KeyReceived(key GroupKey) {
	c.timeKeys.ReplaceOrInsert(key)
}

// The returned slice will be made invalid after following operations on the trigger.
func (c *WatermarkTrigger) Poll() []GroupKey {
	c.outputKeysSlice = c.outputKeysSlice[:0]
	if !c.endOfStreamReached {
		c.timeKeys.Ascend(func(item btree.Item) bool {
			itemTyped, ok := item.(GroupKey)
			if !ok {
				panic(fmt.Sprintf("invalid received item: %v", item))
			}

			if itemTyped[0].Time.After(c.watermark) {
				return false
			}

			c.outputKeysSlice = append(c.outputKeysSlice, itemTyped)
			c.timeKeys.Delete(item)

			return true
		})
	} else {
		c.timeKeys.Ascend(func(item btree.Item) bool {
			itemTyped, ok := item.(GroupKey)
			if !ok {
				panic(fmt.Sprintf("invalid received item: %v", item))
			}

			c.outputKeysSlice = append(c.outputKeysSlice, itemTyped)

			return true
		})
	}
	return c.outputKeysSlice
}
