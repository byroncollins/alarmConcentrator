package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/byroncollins/alarmConcentrator/util"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var options = mqtt.NewClientOptions()

func main() {

	config, err := util.LoadConfig(".")
	if err != nil {
		log.Fatal().Err(err).Msg("cannot load config")
	}

	if config.Environment == "development" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	log.Info().Msg("Starting alarmConcentrator")

	options.AddBroker(config.Broker)
	options.SetClientID("go_alarmConcentrator")
	options.SetDefaultPublishHandler(messagePubHandler)
	options.OnConnect = connectHandler
	options.OnConnectionLost = connectionLostHandler

	listener, err := net.Listen("tcp", fmt.Sprintf(":%s", config.Port))

	if err != nil {
		log.Fatalln(err)
		os.Exit(1)
	}
	defer listener.Close()

	for {
		con, err := listener.Accept()
		if err != nil {
			log.Println(err)
			continue
		}

		// If you want, you can increment a counter here and inject to handleClientRequest below as client identifier
		go handleClientRequest(con)
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

type CSVIPRecord struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	ASDID    int    `json:"asdid"`
	Message  string `json:"message"`
}

func createCSVIPRecord(data [][]string) CSVIPRecord {
	// convert csv lines to array of structs
	var CSVIPString CSVIPRecord
	for i, line := range data {
		if i >= 0 { // omit header line
			var rec CSVIPRecord
			for j, field := range line {
				if j == 0 {
					rec.Name = field
				} else if j == 1 {
					rec.Password = field
				} else if j == 2 {
					var err error
					rec.ASDID, err = strconv.Atoi(field)
					if err != nil {
						continue
					}
				} else if j == 3 {
					rec.Message = field
				}
			}
			CSVIPString = rec
		}
	}
	return CSVIPString
}

var messagePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("Message %s received on topic %s\n", msg.Payload(), msg.Topic())
}

var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
	fmt.Println("Connected")
}

var connectionLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	fmt.Printf("Connection Lost: %s\n", err.Error())
}

func handleClientRequest(conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 1024)

	for {
		// Waiting for the client request
		_, err := conn.Read(buf)
		switch err {
		case nil:
			log.Println(string(buf))
			r := csv.NewReader(strings.NewReader(string(buf)))
			result, err := r.ReadAll()
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf("Lines: %v", len(result))
			fmt.Println()
			for i := range result {
				// Element count.
				fmt.Printf("Elements: %v", len(result[i]))
				fmt.Println()
				// Elements.
				fmt.Println(result[i])
			}

			CSVIPRecord := createCSVIPRecord(result)

			jsonData, err := json.Marshal(CSVIPRecord)
			if err != nil {
				log.Fatal(err)
			}
			data := string(jsonData)

			// remove unicode null char "\u0000", UNLESS escaped, i.e."\\u0000"
			if strings.Contains(data, `\u0000`) {
				log.Printf("[TRACE] null unicode character detected in JSON value - removing if not escaped")
				re := regexp.MustCompile(`((?:^|[^\\])(?:\\\\)*)(?:\\u0000)+`)
				data = re.ReplaceAllString(data, "$1")
			}

			fmt.Println(data)

			client := mqtt.NewClient(options)
			token := client.Connect()
			if token.Wait() && token.Error() != nil {
				panic(token.Error())
			}

			topic := "topic/alarmConcentrator"
			token = client.Publish(topic, 0, false, data)
			token.Wait()

			client.Disconnect(100)

			return
		case io.EOF:
			log.Println("client closed the connection by terminating the process")
			return
		default:
			log.Printf("error: %v\n", err)
			return
		}

		// Responding to the client request
	}
}
