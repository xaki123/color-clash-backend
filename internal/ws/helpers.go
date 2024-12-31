package ws 

import (
	"encoding/json"
	"log"
)

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