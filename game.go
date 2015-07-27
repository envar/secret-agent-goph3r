package main

import (
	"bufio"
	"fmt"
	"log"
	"math/rand"
	"regexp"
)

type Message struct {
	From *Client
	Text string
}

type File struct {
	Filename string
	Size     int
	Secrecy  int
}

type Game struct {
	Name      string
	Clients   map[string]*Client
	Addchan   chan *Client
	Rmchan    chan Client
	Msgchan   chan Message
	isStarted bool
	Filechan  chan File
	Files     []File
}

func prompt(reader *bufio.Reader, writer *bufio.Writer, question string) string {
	if _, err := writer.WriteString(question); err != nil {
		log.Printf("An error occured writing: %s\n", err.Error())
	}

	if err := writer.Flush(); err != nil {
		log.Printf("An error occured flushing: %s\n", err.Error())
	}

	ans, _, err := reader.ReadLine()
	if err != nil {
		log.Printf("An error occured reading: %s\n", err.Error())
	}

	return string(ans)
}

func NewGame(name string) *Game {
	return &Game{
		Name:      name,
		Clients:   make(map[string]*Client),
		Addchan:   make(chan *Client),
		Rmchan:    make(chan Client),
		Msgchan:   make(chan Message),
		isStarted: false,
	}
}

func (g *Game) HandleIO() {
	// handle all io From Clients
	re := regexp.MustCompile(`(?s)(\/\w+)\s(.*)`)
	for {
		select {
		case msg := <-g.Msgchan:
			reResult := re.FindStringSubmatch(msg.Text)
			command := reResult[1]
			if !g.isStarted {
				continue
			}
			switch command {
			case "/help":
				g.help(msg)
			case "/look":
				g.look(msg)
			case "/msg":
				g.SendMsg(msg)
			case "/list":
				msg.From.ListFiles()
			case "/send":
				g.SendFile(msg)
			default:
			}
		case client := <-g.Addchan:
			log.Printf("New client: %p", client)
			g.Clients[client.Nickname] = client
		case client := <-g.Rmchan:
			log.Printf("Client disconnects: %v", client.Conn)
			delete(g.Clients, client.Nickname)
		case file := <-g.Filechan:
			log.Printf("recieved file from filechan")
			g.Files = append(g.Files, file)
		default:
		}
	}
}

func (g *Game) Start() {
	g.loadFilesIntoClients()
	g.isStarted = true
	startMsg := `/msg all * -- | Everyone has arrived, mission starting...
* -- | Ask for /help to get familiar around here
`
	g.Msgchan <- Message{Text: string(startMsg)}
}

func (g *Game) IsFull() bool {
	return len(g.Clients) >= 3
}

func (g *Game) CheckDone() bool {
	for _, c := range g.Clients {
		if !c.DoneSendingFiles {
			return false
		}
	}
	return true
}

func (g *Game) MsgAll(text string) {
	for _, c := range g.Clients {
		c.Ch <- Message{Text: text}
	}
}

func (g *Game) SendMsg(msg Message) {
	re := regexp.MustCompile(`(?s)\/\w+\s(\w+)\s(.*)`)
	reResult := re.FindStringSubmatch(msg.Text)
	if reResult == nil {
		msg.From.Ch <- Message{Text: "Invalid command. Check /help for usage.\n"}
		return
	}
	to := reResult[1]
	text := reResult[2]
	if to == "Glenda" {
		if text == "done\n" {
			msg.From.DoneSendingFiles = true
			doneText := fmt.Sprintf("-- | %s has finished sending files. Waiting for teammates to finish...\n", msg.From.Nickname)
			g.MsgAll(doneText)
			if g.CheckDone() {
				// TODO implement done
				return
			}
		} else {
			msgGlenda(msg.From.Ch)
		}
	} else if to == "all" {
		g.MsgAll(text)
	} else if c, ok := g.Clients[to]; ok {
		c.Ch <- Message{From: msg.From, Text: fmt.Sprintf("%s | %s", msg.From.Nickname, text)}
	} else {
		msg.From.Ch <- Message{Text: fmt.Sprintf("There is no one here named %s.\n", to)}
	}
}

func (g *Game) SendFile(msg Message) {
	re := regexp.MustCompile(`\/\w+\s(\w+)\s(.*)`)
	reResult := re.FindStringSubmatch(msg.Text)
	if reResult == nil {
		msg.From.Ch <- Message{Text: "Invalid command. Check /help for usage.\n"}
		return
	}
	to := reResult[1]
	filename := reResult[2]
	if to == "Glenda" {
		log.Println("sending a file to glenda")
		msg.From.SendFileTo(filename, g.Filechan, true)
		log.Println("sent file")
		if msg.From.Bandwidth < 0 {
			g.Fail()
		}
	} else if c, ok := g.Clients[to]; ok {
		msg.From.SendFileTo(filename, c.Filechan, false)
	} else {
		msg.From.Ch <- Message{Text: fmt.Sprintf("There is no one here named %s.\n", to)}
	}
}

func (g *Game) Fail() {
	for _, c := range g.Clients {
		failText := `fail | You wake up bleary eyed and alone in a concrete box. Your head has a
fail | lump on the side. It seems corporate security noticed you didn't belong,
fail | you should have acted faster. You wonder if you will ever see your
fail | burrow again`
		c.Ch <- Message{Text: string(failText)}
		c.Done <- true
	}
}

func (g *Game) help(msg Message) {
	helpText := `help -- |  Usage:
help -- |
help -- |     /[cmd] [arguments]
help -- |
help -- |  Available commands:
help -- |
help -- |    /msg [to] [text]         send message to coworker
help -- |    /list                    look at files you have access to
help -- |    /send [to] [filename]    move file to coworker
help -- |    /look                    show coworkers
`
	msg.From.Ch <- Message{Text: string(helpText)}
}

func (g *Game) look(msg Message) {
	lookText := "look -- | You look around at your co-workers' nametages:\n"
	for _, c := range g.Clients {
		lookText += ("look -- | " + c.Nickname + "\n")
	}
	lookText += "look -- | Glenda\n"
	msg.From.Ch <- Message{Text: lookText}
}

func msgGlenda(ch chan Message) {
	glendaText := `Glenda | Psst, hey there. I'm going to need your help if we want to exfiltrate
Glenda | these documents. You have clearance that I don't.
Glenda |
Glenda | You each have access to a different set of sensitive files. Within your
Glenda | group you can freely send files to each other for further analysis.
Glenda | However, when sending files to me, the corporate infrastructure team
Glenda | will be alerted if you exceed your transfer quota. Working on too many
Glenda | files will make them suspicious.
Glenda |
Glenda | Please optimize your transfers by the political impact it will create
Glenda | without exceeding any individual transfer quota. The file's security
Glenda | clearance is a good metric to go by for that. Thanks!
Glenda |
Glenda | When each of you is finished sending me files, send me the message
Glenda | 'done'. I'll wait to hear this from all of you before we execute phase
Glenda | two.
`
	ch <- Message{Text: string(glendaText)}
}

func (g *Game) loadFilesIntoClients() {
	capacities := []int{50, 81, 120}
	weights := []int{23, 31, 29, 44, 53, 38, 63, 85, 89, 82}
	profits := []int{92, 57, 49, 68, 60, 43, 67, 84, 86, 72}

	totalFiles := 0

	for {
		for _, client := range g.Clients {
			file := File{
				Filename: fmt.Sprintf("filename_%d.txt", totalFiles),
				Size:     weights[totalFiles],
				Secrecy:  profits[totalFiles],
			}

			client.Files = append(client.Files, file)

			totalFiles++

			if totalFiles >= 10 {
				break
			}
		}

		if totalFiles >= 10 {
			break
		}
	}

	i := 0
	for _, client := range g.Clients {
		client.Bandwidth = capacities[i]
		i++
	}
}

func ShuffleInt(list []int) []int {
	shuffledList := make([]int, len(list))
	perm := rand.Perm(len(list))

	for i, v := range list {
		shuffledList[perm[i]] = v
	}
	return shuffledList
}