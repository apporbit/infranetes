package utils

import (
	"container/list"
	"sync"
)

type Deque struct {
	sync.RWMutex
	deque *list.List
}

func NewDeque() *Deque {
	return &Deque{
		deque: list.New(),
	}
}

func (d *Deque) Append(item interface{}) {
	d.Lock()
	defer d.Unlock()

	d.deque.PushBack(item)
}

func (d *Deque) Prepend(item interface{}) {
	d.Lock()
	defer d.Unlock()

	d.deque.PushFront(item)
}

func (d *Deque) Pop() interface{} {
	d.Lock()
	defer d.Unlock()

	var item interface{} = nil

	elem := d.deque.Back()
	if elem != nil {
		item = d.deque.Remove(elem)
	}

	return item
}

func (d *Deque) Shift() interface{} {
	d.Lock()
	defer d.Unlock()

	var item interface{} = nil

	elem := d.deque.Front()
	if elem != nil {
		item = d.deque.Remove(elem)
	}

	return item
}

func (d *Deque) Back() interface{} {
	d.Lock()
	defer d.Unlock()

	var item interface{} = nil

	elem := d.deque.Back()
	if elem != nil {
		item = elem.Value
	}

	return item
}

func (d *Deque) Front() interface{} {
	d.Lock()
	defer d.Unlock()

	var item interface{} = nil

	elem := d.deque.Front()
	if elem != nil {
		item = elem.Value
	}

	return item
}

func (d *Deque) Size() int {
	d.RLock()
	defer d.RUnlock()

	return d.deque.Len()
}

func (d *Deque) Empty() bool {
	d.RLock()
	defer d.RUnlock()

	return d.deque.Len() == 0
}
