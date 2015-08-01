package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"
)

type Client struct {
	RWC              io.ReadWriteCloser
	Nickname         string
	Msgchan          chan Message
	Files            []File
	Filechan         chan File
	Bandwidth        int
	DoneSendingFiles bool
	Game             *Game
	Done             chan bool
}

func NewClient(rwc io.ReadWriteCloser, nickname string) *Client {
	return &Client{
		RWC:              rwc,
		Nickname:         nickname,
		Msgchan:          make(chan Message, 10), // TODO do I need a buff chan
		Files:            make([]File, 0),
		Filechan:         make(chan File),
		Bandwidth:        100,
		DoneSendingFiles: false,
		Done:             make(chan bool),
		Game:             &Game{},
	}
}

func InitClient(rwc io.ReadWriteCloser, ch chan *Client) {
	bufw := bufio.NewWriter(rwc)

	// Intro message
	if _, err := bufw.WriteString(INTRO_MSG); err != nil {
		log.Printf("Error occured while writing: %s", err.Error())
	}
	if err := bufw.Flush(); err != nil {
		log.Printf("Error occured while flushing: %s\n", err.Error())
	}

	// Get nickname
	nickname, err := GetNickname(rwc)
	if err != nil {
		log.Printf("Error while getting nickname: %s", err.Error())
		return
	}
	client := NewClient(rwc, nickname)

	ch <- client
}

func GetNickname(rw io.ReadWriter) (string, error) {
	bufw := bufio.NewWriter(rw)
	for {
		nickname, err := Prompt(rw, NICK_MSG)
		if err != nil {
			return "", err
		}

		nickname = strings.TrimSpace(nickname)
		if nickname == "" || nickname == "Glenda" {
			if _, err := bufw.WriteString("Invalid Username\n"); err != nil {
				log.Printf("Error occuring while writing: %s\n", err.Error())
				return "", err
			}
			if err := bufw.Flush(); err != nil {
				log.Printf("Error occured while flushing: %s\n", err.Error())
				return "", err
			}
			continue
		}
		return nickname, nil
	}
}

func (c *Client) Start() {
	done := make(chan bool)

	go c.IOHandler(done)
	go c.FileHandler(done)
	log.Printf("Client %s now accepting io", c.Nickname)

	<-c.Done

	done <- true
	done <- true
	c.RWC.Close()
}

func (c *Client) End() {
	c.Game.Rmchan <- c
	c.Done <- true
}

func (c *Client) IOHandler(done chan bool) {
	bufr := bufio.NewReader(c.RWC)
	bufw := bufio.NewWriter(c.RWC)

	// Go routine to handle input, non blocking
	inputchan := make(chan string)
	go func() {
		for {
			line, err := bufr.ReadString('\n')
			log.Printf("Input from %s: %s", c.Nickname, line)
			if err != nil {
				log.Printf("An error occured while reading: %s\n", err.Error())
				c.End()
				return
			}
			inputchan <- line
		}
	}()

	for {
		select {
		case msg := <-c.Msgchan:
			if _, err := bufw.WriteString(msg.Text); err != nil {
				log.Printf("An error occured writing: %s\n", err.Error())
				c.End()
				return
			}
			if err := bufw.Flush(); err != nil {
				log.Printf("An error occured flushing: %s\n", err.Error())
				c.End()
				return
			}
		case input := <-inputchan:
			c.ParseInput(input)
		case <-done:
			return
		default:
		}
	}
}

func (c *Client) ParseInput(input string) {
	re := regexp.MustCompile(`(\/\w+) *(\w*) *(.*)`)
	reResult := re.FindStringSubmatch(input)
	if reResult == nil {
		c.Msgchan <- Message{
			Text: "err -- | Invalid command, try /help to see valid commands\n",
		}
		return
	}
	command := reResult[1]
	arg1 := reResult[2]
	arg2 := reResult[3]
	switch command {
	case "/help":
		log.Printf("in help")
		c.Help()
		log.Printf("out of help")
	case "/msg":
		c.SendMsgTo(arg1, arg2)
	case "/list":
		c.ListFiles()
	case "/send":
		c.SendFileTo(arg1, arg2)
	case "/look":
		c.Look()
	default:
		c.Msgchan <- Message{
			Text: "err -- | Invalid command, try /help to see valid commands\n",
		}
	}
}

func (c *Client) FileHandler(done chan bool) {
	bufw := bufio.NewWriter(c.RWC)
	for {
		select {
		case file := <-c.Filechan:
			if _, err := bufw.WriteString(fmt.Sprintf("send -- | Received file: %s\n", file.Filename)); err != nil {
				log.Printf("An error occured writing: %s\n", err.Error())
			}

			if err := bufw.Flush(); err != nil {
				log.Printf("An error occured flushing: %s\n", err.Error())
			}

			c.Files = append(c.Files, file)
		case <-done:
			return
		}
	}
}

func (c *Client) Help() {
	c.Msgchan <- Message{Text: HELP_MSG}
}

func (c *Client) SendMsgTo(to string, text string) {
	if to == "Glenda" {
		if text == "done" {
			c.Game.ClientDoneChan <- c
			return
		} else {
			c.Msgchan <- Message{Text: GLENDA_MSG}
		}
		return
	}
	for _, client := range c.Game.Clients {
		if to == client.Nickname {
			client.Msgchan <- Message{
				From: c.Nickname,
				To:   to,
				Text: text,
			}
			return
		}
	}
	c.Msgchan <- Message{Text: fmt.Sprintf("err -- | %s does not exist\n", to)}
}

func (c *Client) ListFiles() {
	bufw := bufio.NewWriter(c.RWC)
	_, err := bufw.WriteString(fmt.Sprintf("list -- | Remaining Bandwidth: %d KB\n", c.Bandwidth))
	if err != nil {
		log.Printf("Error occured while writing: %s", err.Error())
	}

	_, err = bufw.WriteString(fmt.Sprintf("list -- | %20s  %8s  %13s\n", "Filename", "Size", "Secrecy Value"))
	for _, f := range c.Files {
		_, err = bufw.WriteString(fmt.Sprintf("list -- | %20s  %5d KB  %13d\n", f.Filename, f.Size, f.Secrecy))
		if err != nil {
			log.Printf("Error occured while writing: %s", err.Error())
		}
	}

	if err := bufw.Flush(); err != nil {
		log.Printf("An error occured flushing: %s\n", err.Error())
	}
}

func (c *Client) SendFileTo(to string, filename string) {
	// TODO rewrite better function
	foundFile := false
	foundClient := false

	var i int
	for j, file := range c.Files {
		if file.Filename == filename {
			foundFile = true
			i = j
			if to == "Glenda" {
				foundClient = true
				c.Game.Filechan <- file
				// Use up bandwidth when sending to Glenda
				c.Bandwidth -= file.Size
				if c.Bandwidth < 0 {
					// fail the game
					c.Game.Status = FAIL
					c.Game.End()
				}
				c.Msgchan <- Message{
					Text: fmt.Sprintf("send -- | Sent file: %s", file.Filename),
				}
			}
			for _, client := range c.Game.Clients {
				if to == client.Nickname {
					foundClient = true
					client.Filechan <- file
					break
				}
			}
			break
		}
	}

	if !foundFile {
		c.Msgchan <- Message{
			Text: fmt.Sprintf("err -- | Error sending file: file \"%s\" does not exist", filename),
		}
		return
	}
	if !foundClient {
		c.Msgchan <- Message{
			Text: fmt.Sprintf("err -- | Error sending file: client \"%s\" does not exist", to),
		}
		return
	}

	files := c.Files
	newFiles := make([]File, 0, len(files)-1)
	if i == 0 {
		newFiles = files[1:]
	} else if i == len(files)-1 {
		newFiles = files[:len(files)-1]
	} else {
		newFiles = append(files[:i-1], files[i:]...)
	}
	c.Files = newFiles
}

func (c *Client) Look() {
	lookText := "look -- | You look around at your co-workers' nametages:\n"
	for _, client := range c.Game.Clients {
		lookText += ("look -- | " + client.Nickname + "\n")
	}
	lookText += "look -- | Glenda\n"
	c.Msgchan <- Message{Text: lookText}
}
