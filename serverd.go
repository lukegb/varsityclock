package main

import (
	"github.com/gorilla/websocket"
	"log"
	"net/http"
)

type MessageOverlord struct {
	Register chan chan WSMessage
	Input    chan WSMessage

	registeredOutputs []chan WSMessage

	lastSentMessage *WSMessage
}

type WSMessage struct {
	messageType int
	message     []byte
}

var m *MessageOverlord

func (m *MessageOverlord) mainLoop() {
	m.registeredOutputs = make([]chan WSMessage, 0)
	m.Register = make(chan chan WSMessage)
	m.Input = make(chan WSMessage)

	log.Println("messageoverlord ready")

	for {
		select {
		case r := <-m.Register:
			m.registeredOutputs = append(m.registeredOutputs, r)
			break
		case i := <-m.Input:
			log.Println("messageoverlord shuttling message")
			m.lastSentMessage = &i
			newOutputs := make([]chan WSMessage, 0, len(m.registeredOutputs))
			for _, c := range m.registeredOutputs {
				select {
				case c <- i:
					newOutputs = append(newOutputs, c)
					break
				default:
					log.Println("failed to write message to channel :( deregistering")
					close(c)
					break
				}
			}
			m.registeredOutputs = newOutputs
		}
	}
}

type InitMessage struct {
	ConnectionType string `json:"type"` // "control" or "display"
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func controlConnectionHandler(w http.ResponseWriter, r *http.Request, conn *websocket.Conn) {
	if m.lastSentMessage != nil {
		msg := m.lastSentMessage
		log.Println("control: sending LSM", msg)
		if w, err := conn.NextWriter(msg.messageType); err != nil {
			log.Println("control: NextWriter err was " + err.Error() + ", stopping")
			conn.Close()
			return
		} else {
			if _, err := w.Write(msg.message); err != nil {
				log.Println("control: failed to write, stopping")
				conn.Close()
				return
			}
			w.Close()
		}
	}

	for {
		messageType, p, err := conn.ReadMessage()
		if err != nil {
			log.Println("control: error was " + err.Error() + ", stopping")
			conn.Close()
			return
		}

		wsm := WSMessage{
			messageType: messageType,
			message:     p,
		}
		m.Input <- wsm
	}
}

func displayConnectionHandler(w http.ResponseWriter, r *http.Request, conn *websocket.Conn) {
	go func(c *websocket.Conn) {
		for {
			if _, _, err := conn.NextReader(); err != nil {
				conn.Close()
				break
			}
		}
	}(conn)

	if m.lastSentMessage != nil {
		msg := m.lastSentMessage
		log.Println("display: sending LSM", msg)
		if w, err := conn.NextWriter(msg.messageType); err != nil {
			log.Println("display: NextWriter err was " + err.Error() + ", stopping")
			conn.Close()
			return
		} else {
			if _, err := w.Write(msg.message); err != nil {
				log.Println("display: failed to write, stopping")
				conn.Close()
				return
			}
			w.Close()
		}
	}

	c := make(chan WSMessage, 1)
	m.Register <- c

	for {
		select {
		case msg := <-c:
			if msg.message == nil {
				log.Println("told to close, stopping")
				conn.Close()
				return
			}
			if w, err := conn.NextWriter(msg.messageType); err != nil {
				log.Println("NextWriter err was " + err.Error() + ", stopping")
				conn.Close()
				return
			} else {
				if _, err := w.Write(msg.message); err != nil {
					log.Println("failed to write entire message, stopping")
					conn.Close()
					return
				}
				w.Close()
			}
		}
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	initMsg := InitMessage{}
	if err := conn.ReadJSON(&initMsg); err != nil {
		log.Println(err)
		return
	}

	if initMsg.ConnectionType == "control" {
		controlConnectionHandler(w, r, conn)
	} else if initMsg.ConnectionType == "display" {
		displayConnectionHandler(w, r, conn)
	} else {
		log.Println("Unknown connection type " + initMsg.ConnectionType)
		return
	}
}

func main() {
	m = new(MessageOverlord)
	go m.mainLoop()

	http.HandleFunc("/ws", handler)
	http.Handle("/", http.FileServer(http.Dir("html/")))
	log.Fatalln(http.ListenAndServe(":8000", nil))
}
