package ws 

import (
	"time"
	"context"
	"sync"
	"log"
	"fmt"
	"github.com/google/uuid"
)

type Room struct {
	players map[string]*Player
	grids []*Grid
	grids_count map[string]int

	threadMethodsStopped chan struct{}

	winnerFound chan []byte
	winnerBroadcasted chan struct{}

	startTime time.Time
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

func NewRoom(h *Hub) *Room{
	ctx,cancel := context.WithCancel(context.Background())

    now := time.Now()
    startTime := now.Truncate(time.Second)
	
	room := &Room{
		players: make(map[string]*Player),
		grids: []*Grid{},
		grids_count: map[string]int{"red":1,"yellow":1,"green":1,"blue":1,"pink":1,"orange":1,"grey":1},
		timeLeft: 30,
		startTime: startTime,
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
			room.grids = append(room.grids,&Grid{X:x*room.grid_size,Y:y*room.grid_size,Color:"white",dissapearTime:time.Second*4})
			room.grids_mu.Unlock()
		}
	}

	return room
}

func (r *Room) GetCurrentTimeLeft() int {
        elapsed := time.Since(r.startTime)
        remaining := 30 - int(elapsed.Seconds())
        if remaining < 0 {
                return 0
        }
        return remaining
}

func (r *Room) Ticker() {
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

func (r *Room) GetWinner() {
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
            fmt.Sprintf("the winner is %s with %d points", maxCountsOwner, maxCount))
    } else {
        message, err = EventJSON("winner", "nobody wins, it's a tie")
    }

    if err != nil {
        log.Println("Error creating winner message:", err)
        return
    }

    r.winnerFound <- message
}

func (r *Room) Run() {
	go r.Ticker()

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
            r.timeLeft = r.GetCurrentTimeLeft()
            r.time_left_mu.Unlock()

            timerUpdate, err := EventJSON("timer_update", map[string]interface{}{
                    "time_left": r.timeLeft,
            })

            if err == nil {
            	go func(){

                    r.broadcast <- timerUpdate
                }()
            }

		case <-game_end_ticker.C:
			game_end_ticker.Stop()		

			go func(){
				r.GetWinner()
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

func (r *Room) ChangeGridColor(x int, y int, color string, client *Client) {
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