package ws

import (
	"sync"

	"metrochat/internal/secure"
)

type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[*Client]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: make(map[string]map[*Client]struct{})}
}

func (h *Hub) Add(userID string, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	set, ok := h.clients[userID]
	if !ok {
		set = make(map[*Client]struct{})
		h.clients[userID] = set
	}
	set[c] = struct{}{}
}

func (h *Hub) Remove(userID string, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	set, ok := h.clients[userID]
	if !ok {
		return
	}
	if _, exists := set[c]; !exists {
		return
	}
	delete(set, c)

	// 使用sync.Once防止重复关闭channel
	c.closeOnce.Do(func() {
		close(c.Send)
	})

	if len(set) == 0 {
		delete(h.clients, userID)
	}
}

func (h *Hub) BroadcastToUser(userID string, msg []byte) {
	h.mu.RLock()
	// 在锁内复制客户端列表，防止并发修改
	var clients []*Client
	if set, ok := h.clients[userID]; ok {
		clients = make([]*Client, 0, len(set))
		for c := range set {
			clients = append(clients, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range clients {
		payload := msg
		if len(c.EncKey) > 0 && len(c.MacKey) > 0 {
			enc, err := secure.EncryptWithKeys(msg, c.EncKey, c.MacKey)
			if err != nil {
				continue
			}
			payload = enc
		}
		select {
		case c.Send <- payload:
		default:
			// Drop if the client is slow to avoid blocking.
		}
	}
}

func (h *Hub) Broadcast(msg []byte) {
	h.mu.RLock()
	// 在锁内复制客户端列表，防止并发修改
	allClients := make([]*Client, 0)
	for _, set := range h.clients {
		for c := range set {
			allClients = append(allClients, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range allClients {
		payload := msg
		if len(c.EncKey) > 0 && len(c.MacKey) > 0 {
			enc, err := secure.EncryptWithKeys(msg, c.EncKey, c.MacKey)
			if err != nil {
				continue
			}
			payload = enc
		}
		select {
		case c.Send <- payload:
		default:
		}
	}
}
