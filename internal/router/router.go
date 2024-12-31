package router 

import (
	"net/http"
	"colback/internal/ws"
)

func New(hub *ws.Hub) *http.ServeMux{
	mux := http.NewServeMux()

	wsHandler := ws.NewHandler(hub)

	mux.HandleFunc("/ws",wsHandler.ServeWs)

	return mux
}	