package ws 

import (
	"sync"
	"log"
)

type Hub struct {
	clients map[string]*Client
	rooms map[string]*Room

	broadcast chan []byte
	register chan *Client
	unregister chan *Client

	registerRoom chan *Room
	unregisterRoom chan *Room

	client_mu sync.RWMutex
	room_mu sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[string]*Client),

		registerRoom: make(chan *Room),
		unregisterRoom: make(chan *Room),
		rooms: make(map[string]*Room),
	}
}


func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.client_mu.Lock()
			h.clients[client.id] = client
			h.client_mu.Unlock()

		case client := <-h.unregister:
			h.client_mu.Lock()
			if _, ok := h.clients[client.id]; ok {

				client.mu.RLock()
				if client.player != nil {
					log.Println("client is in room")	
					client.player.room.unregister <- client.player
				}
				client.mu.RUnlock()

				delete(h.clients, client.id)
				close(client.send)

				log.Println("client gone")
			}
			h.client_mu.Unlock()

		case message := <-h.broadcast:
			h.client_mu.RLock()
			for _, client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					h.client_mu.Lock()
					delete(h.clients, client.id)
					h.client_mu.Unlock()
				}
			}
			h.client_mu.RUnlock()

		case room := <-h.registerRoom:
			log.Println("Registering new room with ID:", room.id)
			h.room_mu.Lock()
			h.rooms[room.id] = room
			h.room_mu.Unlock()
			go room.Run()

			log.Println("Room sucessfully added: ",room.id)
			log.Println(h.rooms)

			room.joinedHub <- struct{}{}

		case room := <-h.unregisterRoom:
			h.room_mu.Lock()
			if _, ok := h.rooms[room.id]; ok {
				room.cancel()
				<-room.threadMethodsStopped
				<-room.threadMethodsStopped
				delete(h.rooms, room.id)
				close(room.register)
				close(room.unregister)
				close(room.broadcast)
			}
			h.room_mu.Unlock()

			log.Println("room removed: ",room.id)
		}
	}
}