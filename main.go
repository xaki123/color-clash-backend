package main

import (
	"net/http"
	"sync"
	"encoding/json"
	"log"
	"fmt"
	"time"
	"context"
	"github.com/gorilla/websocket"
	"github.com/google/uuid"
)

var
(
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	colors = []string{"red","yellow","blue","green","pink","orange","grey"}
)
const (
	pongWait = 5 * time.Minute
	pingPeriod = (pongWait * 9) / 10
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

type Grid struct {
	X int `json:"x"`
	Y int `json:"y"`
	Color string `json:"color"`
	dissapearTime time.Duration
}

type Room struct {
	players map[string]*Player
	grids []*Grid
	grids_count map[string]int

	threadMethodsStopped chan struct{}

	winnerFound chan []byte
	winnerBroadcasted chan struct{}

	timeLeft int

	grid_size int
	gameLasts time.Duration

	broadcast chan []byte
	register chan *Player
	unregister chan *Player
	joinedHub chan struct{}
	stopTicker chan struct{}
	id string
	hub *Hub
	maxLength int

	width int 
	height int

	previoslyLeftPlayerColor string

	context context.Context
	cancel context.CancelFunc

	mu sync.RWMutex
	grids_mu sync.RWMutex
	grids_count_mu sync.RWMutex
	time_left_mu sync.RWMutex
	left_color_mu sync.RWMutex
}

func newRoom(h *Hub) *Room{
	ctx,cancel := context.WithCancel(context.Background())
	
	room := &Room{
		players: make(map[string]*Player),
		grids: []*Grid{},
		grids_count: map[string]int{"red":1,"yellow":1,"green":1,"blue":1,"pink":1,"orange":1,"grey":1},
		timeLeft: 30,
		winnerFound: make(chan []byte),
		winnerBroadcasted: make(chan struct{}),
		threadMethodsStopped: make(chan struct{}),

		grid_size: 130,
		gameLasts: time.Second * 32,

		broadcast: make(chan []byte),
		register: make(chan *Player),
		unregister: make(chan *Player),
		joinedHub: make(chan struct{}),
		stopTicker: make(chan struct{}),
		id: uuid.New().String(),
		hub:h,
		maxLength: len(colors),
		width:910,
		height:650,
		context:ctx,
		cancel:cancel,
	}

	for x := 0; x < room.width/room.grid_size; x++ {
		for y := 0; y < room.height/room.grid_size; y++{
			room.grids_mu.Lock()
			room.grids = append(room.grids,&Grid{X:x*room.grid_size,Y:y*room.grid_size,Color:"white",dissapearTime:time.Second*2})
			room.grids_mu.Unlock()
		}
	}

	return room
}
func (r *Room) ticker() {
    ticker := time.NewTicker(time.Millisecond * 15)
    defer ticker.Stop()
    
    for {
        select {
        case <-r.context.Done():
            log.Println("[ticker] DONEEE")
            defer func (){
                r.threadMethodsStopped <- struct{}{}
            }()
            return
        case <-ticker.C:
            r.mu.Lock()
            r.grids_mu.Lock()

            playersCopy := make(map[string]*Player)
            for k, v := range r.players {
            	v.mu.RLock()
                playersCopy[k] = v
            	v.mu.RUnlock()
            }

            gridsCopy := make([]*Grid, len(r.grids))
            copy(gridsCopy, r.grids)

            b, err := EventJSON("tick", map[string]interface{}{
                "players": playersCopy, 
                "grids": gridsCopy,
            })

            r.mu.Unlock()
            r.grids_mu.Unlock()

            if err != nil {
                log.Println("[ticker] error in eventjson", err)
            }

            r.broadcast <- b
        }
    }
}

func (r *Room) getWinner() {
    r.grids_count_mu.RLock()
    defer r.grids_count_mu.RUnlock()

    maxCount := 0 
    maxCountsOwner := ""
    tie := false

    for color, count := range r.grids_count {
        switch {
        case count > maxCount:
            maxCount = count
            maxCountsOwner = color
            tie = false
        case count == maxCount:
            tie = true
        case count < maxCount:
        	continue
        }
    } 

    var message []byte
    var err error
    if !tie {
        message, err = EventJSON("winner", 
            fmt.Sprintf("the winner is %s with %d points", maxCountsOwner, maxCount-1))
    } else {
        message, err = EventJSON("winner", "nobody wins, it's a tie")
    }

    if err != nil {
        log.Println("Error creating winner message:", err)
        return
    }

    r.winnerFound <- message
}

func (r *Room) run() {
	go r.ticker()

	game_end_ticker := time.NewTicker(r.gameLasts)
	defer game_end_ticker.Stop()

	timer := time.NewTicker(time.Second*1)
	defer timer.Stop()

	for {
		select {
		case <-r.context.Done():
			log.Println("[run] DONEEE")
			defer func (){
				r.threadMethodsStopped <- struct{}{}
			}()
			return

		case <-timer.C:
			r.time_left_mu.Lock()
			r.timeLeft -= 1
			r.time_left_mu.Unlock()

		case <-game_end_ticker.C:
			game_end_ticker.Stop()		

			go func(){
				r.getWinner()
				<-r.winnerBroadcasted
				log.Println("winner broadcasted to everyone")

				r.hub.unregisterRoom <- r
			}()


		case player := <-r.register:
			log.Println("new player recieved in a room: ",r)
			
			r.mu.Lock()
			r.players[player.c.id] = player
			r.mu.Unlock()

			log.Println("[room.run] player added to the room")
			player.joinedRoom<-struct{}{}

		case player := <-r.unregister:
			log.Println("player unregister recieved: ",player)

			r.left_color_mu.Lock()
			r.previoslyLeftPlayerColor = player.Color
			r.left_color_mu.Unlock()

			r.mu.Lock()
			delete(r.players,player.c.id)
			r.mu.Unlock()

			log.Println("mawoni: ",len(r.players))
			if len(r.players) == 0 {
				r.hub.unregisterRoom <- r			
			}
		case message := <-r.broadcast:
			select {
				case m:=<-r.winnerFound:	
					r.mu.RLock()
					for _, player := range r.players {
						player.c.send <- m

						player.c.mu.Lock()
						player.c.player = nil
						player.c.mu.Unlock()
					}
					r.mu.RUnlock()			
					go func(){
						r.winnerBroadcasted <- struct{}{}
					}()
				default:
					r.mu.RLock()
					for _, player := range r.players {
						player.c.send <- message
					}
					r.mu.RUnlock()
			}
		}
	}
}

type Player struct {
	c *Client
	Color string `json:"color"`
	X int `json:"x"`
	Y int `json:"y"`
	Width int `json:"width"`
	Height int `json:"height"`
	speed int
	joinedRoom chan struct{}
	room *Room
	Points int `json:"points"`

	mu sync.RWMutex
}

func newPlayer(c *Client, r *Room, color string, x int) *Player {
	return &Player{
		c:c,
		Color:color,
		X: x,
		Y: r.height - r.grid_size,
		Width: r.grid_size,
		Height: r.grid_size,
		speed: r.grid_size,
		joinedRoom: make(chan struct{}),
		room: r,
		Points: 0,
	}
}

func newHub() *Hub {
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


func (h *Hub) run() {
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
			go room.run()

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

type Client struct {
	hub *Hub
	conn *websocket.Conn
	send chan []byte
	id string
	player *Player

	mu sync.RWMutex
}

func newClient(hub *Hub, conn *websocket.Conn) *Client{
	return &Client{
		hub:hub,
		conn:conn,
		send:make(chan []byte),
		id: uuid.New().String(),
	}
}

type Event struct {
	Name string `json:"eventName"`
	Data interface{} `json:"eventData"`
}

func EventJSON(eventName string, eventData interface{}) ([]byte, error){
	var e Event 
	e.Name = eventName
	e.Data = eventData

	b,err := json.Marshal(e)
	if err != nil {
		log.Println("error marshaling json: ",err)
		return nil,err
	}

	return b,nil
}

func (e Event) createRoom(c *Client) {
	room := newRoom(c.hub)
	c.hub.registerRoom <- room
	<-room.joinedHub

	roomLength := len(room.players)
	player := newPlayer(c,room,colors[roomLength],room.grid_size*len(room.players))
	
	c.mu.Lock()
	c.player = player
	c.mu.Unlock()

	room.register <- player
	<-player.joinedRoom

	log.Println("[create_room] user joined the room")

	room.time_left_mu.RLock()
	message,err := EventJSON("get_token",map[string]interface{}{"room_id":room.id,"my_id":c.id,"canvas_width":room.width,"canvas_height":room.height,"time_left":room.timeLeft,"grid_size":room.grid_size})
	room.time_left_mu.RUnlock()

	if err != nil {
		log.Println("error sending marshaling json: ",err)
		return
	}

	c.send <- message
}

func (e Event) joinRoom(c *Client) {
	if e.Data == nil {return}
	data := e.Data.(string)

	if room, ok := c.hub.rooms[data]; ok {
		roomLength := len(room.players)
		if(roomLength) == room.maxLength {
			return
		}

		var color string 
		var x int

		room.left_color_mu.RLock()

		if room.previoslyLeftPlayerColor != "" {
			color = room.previoslyLeftPlayerColor
			for k,v := range colors {
				if v == color {
					x = room.grid_size*k
				}
			}

			room.previoslyLeftPlayerColor = ""
		}else {
			color = colors[roomLength]
			x = room.grid_size*len(room.players)
		}

		room.left_color_mu.RUnlock()

		player := newPlayer(c,room,color,x)
		
		c.mu.Lock()
		c.player = player 
		c.mu.Unlock()

		room.register <- player
		<-player.joinedRoom

		log.Println("[joinRoom] player joined the room: ",room.id)

		room.time_left_mu.RLock()
		message,err := EventJSON("get_token",map[string]interface{}{"room_id":room.id,"my_id":c.id,"canvas_width":room.width,"canvas_height":room.height,"time_left":room.timeLeft,"grid_size":room.grid_size})
		room.time_left_mu.RUnlock()

		if err != nil {
			log.Println("error sending marshaling json: ",err)
			return
		}

		c.send <- message

	}else {
		d,err := EventJSON("warning","wrong id")
		if err != nil {
			return
		}
		c.send <- d
	}

}

func (r *Room) changeGridColor(x int, y int, color string, client *Client) {
	r.grids_mu.Lock()
	for _,grid := range r.grids {
		if grid.X == x && grid.Y == y {
			
			if grid.Color != "white" {
				r.grids_mu.Unlock()
				return
			}

			grid.Color = color
			currentGrid := grid

			r.grids_count_mu.Lock()
			r.grids_count[color]++
			r.grids_count_mu.Unlock()

			client.player.mu.Lock()
			client.player.Points++
			client.player.mu.Unlock()

			go func(ctx context.Context){
			    select {
			    case <-ctx.Done():
			        return
			    case <-time.After(currentGrid.dissapearTime):
			        r.grids_mu.Lock()
			        currentGrid.Color = "white"
			        
			        r.grids_count_mu.Lock()
			        r.grids_count[color]--
			        r.grids_count_mu.Unlock()

			        r.grids_mu.Unlock()

			        client.player.mu.Lock()
			        client.player.Points--
			        client.player.mu.Unlock()
			    }
			}(r.context)
		}
	}
	r.grids_mu.Unlock()
}

func (e Event) handleKeyDown(c *Client) {
    c.mu.Lock()
    defer c.mu.Unlock()

    if c.player != nil {
        c.player.room.mu.Lock()
        defer c.player.room.mu.Unlock()

        prev_x := c.player.X
        prev_y := c.player.Y
        color := c.player.Color
        room_width := c.player.room.width
        room_height := c.player.room.height

        switch data := e.Data.(string); data {
        case "d":
            if (c.player.X + c.player.Width) < room_width {
                c.player.X += c.player.speed
                c.player.room.changeGridColor(prev_x, prev_y,color,c)
            }
        case "a":
            if (c.player.X) > 0 {
                c.player.X -= c.player.speed
                c.player.room.changeGridColor(prev_x, prev_y, color,c)
            }
        case "w":
            if (c.player.Y) > 0 {
                c.player.Y -= c.player.speed
                c.player.room.changeGridColor(prev_x, prev_y, color,c)
            }
        case "s":
            if (c.player.Y + c.player.Height) < room_height {
                c.player.Y += c.player.speed
                c.player.room.changeGridColor(prev_x, prev_y, color,c)
            }
        }
    }
}

func (c *Client) eventHandler(message []byte) {
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

func (c *Client) readPump() {
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

		go c.eventHandler(message)
	}
}

func (c *Client) writePump() {
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

func serveWs(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println(err)
			return
		}

		client := newClient(hub,conn)
		client.hub.register <- client

		go client.writePump()
		go client.readPump()

		log.Println("new user joined")
	}
} 	

func main(){
	mux := http.NewServeMux()
	server := &http.Server{
		Addr:":6969",
		Handler:mux,
	}

	hub := newHub()
	go hub.run()

	mux.HandleFunc("/ws",serveWs(hub))

	go func() {
		log.Println("server running")
	}()
	server.ListenAndServe()
}
