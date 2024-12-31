package ws 

import "time"

type Grid struct {
	X int `json:"x"`
	Y int `json:"y"`
	Color string `json:"color"`
	dissapearTime time.Duration
}