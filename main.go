package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	debug := flag.Bool("debug", false, "sets log level to debug")
	flag.Parse()

	// Default level for this example is info, unless debug flag is present
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

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

	listener, err := net.Listen("tcp4", ":"+strconv.Itoa(config.Port))

	if err != nil {
		log.Fatal().Err(err).Msg("cannot create listener")
		os.Exit(1)
	}
	defer listener.Close()

	for {
		con, err := listener.Accept()
		if err != nil {
			log.Fatal().Err(err).Msg("listener.Accept failed")
			continue
		}

		// If you want, you can increment a counter here and inject to handleClientRequest below as client identifier
		go handleClientRequest(con)
	}
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

func sendMqttMessage(data string, topic string) {
	client := mqtt.NewClient(options)
	token := client.Connect()
	if token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	token = client.Publish(topic, 0, false, data)
	token.Wait()

	client.Disconnect(100)
}

var messagePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	log.Debug().Msg(fmt.Sprintf("Message %s received on topic %s\n", msg.Payload(), msg.Topic()))
}

var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
	log.Debug().Msg("Connected")
}

var connectionLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	log.Error().Err(err).Msg("Connection Lost")
}

func handleClientRequest(conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 1024)

	for {
		// Waiting for the client request
		_, err := conn.Read(buf)
		switch err {
		case nil:
			log.Debug().Msg(string(buf))
			r := csv.NewReader(strings.NewReader(string(buf)))
			result, err := r.ReadAll()
			if err != nil {
				log.Error().Err(err).Msg("Error reading CSV")
			}
			log.Debug().Msg(fmt.Sprintf("Lines: %v", len(result)))
			for i := range result {
				// Element count.
				log.Debug().Msg(fmt.Sprintf("Element Count: %v", len(result[i])))
				// Elements.
				log.Debug().Msg(fmt.Sprintf("Elements: %s", result[i]))
			}

			CSVIPRecord := createCSVIPRecord(result)

			jsonData, err := json.Marshal(CSVIPRecord)
			if err != nil {
				log.Error().Err(err).Msg("Error marshalling JSON")
			}
			data := string(jsonData)

			// remove unicode null char "\u0000", UNLESS escaped, i.e."\\u0000"
			if strings.Contains(data, `\u0000`) {
				log.Debug().Msg("[TRACE] null unicode character detected in JSON value - removing if not escaped")
				re := regexp.MustCompile(`((?:^|[^\\])(?:\\\\)*)(?:\\u0000)+`)
				data = re.ReplaceAllString(data, "$1")
			}
			log.Debug().Msg(fmt.Sprintf(data))

			//send data over Mqtt
			sendMqttMessage(data, "topic/alarmConcentrator")

			// Responding to the client request
			// Send sent message in response (kiss off)
			conn.Write([]byte(buf))

			return
		case io.EOF:
			log.Debug().Msg("client closed the connection by terminating the process")
			return
		default:
			log.Error().Err(err).Msg("error")
			return
		}

	}
}
