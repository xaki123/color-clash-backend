package ws 

import (
	"net/http"
	"log"
	"github.com/gorilla/websocket"
)

var (
	upgrader = websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},			
		}
)

type Handler struct {
	hub *Hub
}

func NewHandler(hub *Hub) *Handler {
	return &Handler{
		hub:hub,
	}
}

func (handler *Handler) ServeWs(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	client := NewClient(handler.hub,conn)
	client.hub.register <- client

	go client.WritePump()
	go client.ReadPump()

	log.Println("new user joined")
} 