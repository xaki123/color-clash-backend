package ws 

import (
	"log"
)

type Event struct {
	Name string `json:"eventName"`
	Data interface{} `json:"eventData"`
}

func (e Event) createRoom(c *Client) {
	room := NewRoom(c.hub)
	c.hub.registerRoom <- room
	<-room.joinedHub

	roomLength := len(room.players)
	player := NewPlayer(c,room,colors[roomLength],room.grid_size*len(room.players))
	
	c.mu.Lock()
	c.player = player
	c.mu.Unlock()

	room.register <- player
	<-player.joinedRoom

	log.Println("[create_room] user joined the room")

	currentTimeLeft := room.GetCurrentTimeLeft()
	message,err := EventJSON("get_token",map[string]interface{}{"room_id":room.id,"my_id":c.id,"canvas_width":room.width,"canvas_height":room.height,"time_left":currentTimeLeft,"grid_size":room.grid_size})

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

		player := NewPlayer(c,room,color,x)
		
		c.mu.Lock()
		c.player = player 
		c.mu.Unlock()

		room.register <- player
		<-player.joinedRoom

		log.Println("[joinRoom] player joined the room: ",room.id)

		currentTimeLeft := room.GetCurrentTimeLeft()
		message,err := EventJSON("get_token",map[string]interface{}{"room_id":room.id,"my_id":c.id,"canvas_width":room.width,"canvas_height":room.height,"time_left":currentTimeLeft,"grid_size":room.grid_size})

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
                c.player.room.ChangeGridColor(prev_x, prev_y,color,c)
            }
        case "a":
            if (c.player.X) > 0 {
                c.player.X -= c.player.speed
                c.player.room.ChangeGridColor(prev_x, prev_y, color,c)
            }
        case "w":
            if (c.player.Y) > 0 {
                c.player.Y -= c.player.speed
                c.player.room.ChangeGridColor(prev_x, prev_y, color,c)
            }
        case "s":
            if (c.player.Y + c.player.Height) < room_height {
                c.player.Y += c.player.speed
                c.player.room.ChangeGridColor(prev_x, prev_y, color,c)
            }
        }
    }
}