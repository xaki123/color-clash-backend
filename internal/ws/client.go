package ws 

import (
	"sync"
	"time"
	"log"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	pongWait = 5 * time.Minute
	pingPeriod = (pongWait * 9) / 10
)

type Client struct {
	hub *Hub
	conn *websocket.Conn
	send chan []byte
	id string
	player *Player

	mu sync.RWMutex
}

func NewClient(hub *Hub, conn *websocket.Conn) *Client{
	return &Client{
		hub:hub,
		conn:conn,
		send:make(chan []byte),
		id: uuid.New().String(),
	}
}

func (c *Client) EventHandler(message []byte) {
	var event Event

	if err := json.Unmarshal(message,&event); err != nil {
		log.Println(err)
		return
	}

	switch event.Name {
	case "create_room":
		event.createRoom(c)
	case "join_room":
		event.joinRoom(c)
	case "keydown":
		event.handleKeyDown(c)
	}

}

func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func (d string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}

		go c.EventHandler(message)
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage,message); err != nil {
				log.Println("[writePump] error while sending message: ",err)
			}
		case <-ticker.C:
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Println("error sending ping message: ",err)
				return
			}
		}
	}
}