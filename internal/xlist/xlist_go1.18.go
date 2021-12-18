//go:build go1.18

package xlist

type List[T any] struct {
	front *Node[T]
	back  *Node[T]
	size  int
}

func (l *List[T]) Len() int        { return l.size }
func (l *List[T]) Front() *Node[T] { return l.front }
func (l *List[T]) Back() *Node[T]  { return l.back }

func (l *List[T]) Clear() { l.front = nil; l.back = nil; l.size = 0 }

func (l *List[T]) PushFront(value T) *Node[T] {
	node := &Node[T]{
		next:  l.front,
		Value: value,
	}
	if l.front != nil {
		l.front.prev = node
	}
	l.front = node
	if l.back == nil {
		l.front = node
	}
	l.size++
	return node
}

func (l *List[T]) PushBack(value T) *Node[T] {
	node := &Node[T]{
		prev:  l.back,
		Value: value,
	}
	if l.back != nil {
		l.back.next = node
	}
	l.back = node
	if l.front == nil {
		l.front = node
	}
	l.size++
	return node
}

func (l *List[T]) InsertBefore(value T, mark *Node[T]) *Node[T] {
	node := &Node[T]{
		Value: value,
	}
	node.prev = mark.prev
	if node.prev != nil {
		node.prev.next = node
	}
	mark.prev = node
	node.next = mark
	if l.front == mark {
		l.front = node
	}
	l.size++
	return node
}

func (l *List[T]) InsertAfter(value T, mark *Node[T]) *Node[T] {
	node := &Node[T]{
		Value: value,
	}
	node.next = mark.next
	if node.next != nil {
		node.next.prev = node
	}
	mark.next = node
	node.prev = mark
	if l.back == mark {
		l.back = node
	}
	l.size++
	return node
}

func (l *List[T]) Remove(node *Node[T]) {
	l.remove(node)
	l.size--
}

func (l *List[T]) remove(node *Node[T]) {
	if l.front == node {
		l.front = l.front.next
	} else {
		node.prev.next = node.next
	}
	if l.back == node {
		l.back = l.back.prev
	} else {
		node.next.prev = node.prev
	}

	node.prev = nil
	node.next = nil
}

func (l *List[T]) MoveBefore(node *Node[T], mark *Node[T]) {
	l.remove(node)
	node.prev = mark.prev
	mark.prev = node
	node.next = mark
	if l.front == mark {
		l.front = node
	}
}

func (l *List[T]) MoveAfter(node *Node[T], mark *Node[T]) {
	l.remove(node)
	node.next = mark.next
	mark.next = node
	node.prev = mark
	if l.back == mark {
		l.back = node
	}
}

func (l *List[T]) MoveToFront(node *Node[T]) {
	if l.front == node {
		return
	}
	node.prev.next = node.next
	if node.next != nil {
		node.next.prev = node.prev
	}
	node.next = l.front.next
	node.prev = nil
	l.front = node
}

func (l *List[T]) MoveToBack(node *Node[T]) {
	if l.back == node {
		return
	}
	node.next.prev = node.prev
	if node.prev != nil {
		node.prev.next = node.next
	}
	node.prev = l.back.prev
	node.next = nil
	l.back = node
}

type Node[T any] struct {
	prev  *Node[T]
	next  *Node[T]
	Value T
}

func (n *Node[T]) Next() *Node[T] { return n.next }
func (n *Node[T]) Prev() *Node[T] { return n.prev }
