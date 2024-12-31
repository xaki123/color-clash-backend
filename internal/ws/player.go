package ws 

import (
	"sync"
)

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

func NewPlayer(c *Client, r *Room, color string, x int) *Player {
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
		Points: 1,
	}
}