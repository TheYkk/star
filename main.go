package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	webhook "gopkg.in/go-playground/webhooks.v5/github"

	"math"
	"time"

	"net/http"
	"os"
	"strconv"
)

// ? Version of build
var (
	Version = "dev"
)

var port = flag.String("port",getenv("PORT", strconv.Itoa(8080)),"Port to listen on for HTTP")
var printVersion = flag.Bool("v",false,"Print version")
var help = flag.Bool("help",false,"Get Help")
var listen = flag.String("listen", getenv("LISTEN", "0.0.0.0"), "IPv4 address to listen on")

func init() {

	flag.Usage = func() {
		flag.PrintDefaults()
		os.Exit(0)
	}

	flag.Parse()

	if *help {
		flag.PrintDefaults()
		os.Exit(0)
	}

	if *printVersion {
		fmt.Print(Version)
		os.Exit(0)
	}

	log.SetFormatter(&log.JSONFormatter{
		PrettyPrint: true,
	})
	log.SetReportCaller(true)
}

func main() {
	log.Printf("Init Star %s", Version)

	secret := getenv("GITHUB_SECRET", "")
	if secret == "" {
		log.Fatal("Github webhook secret not set")
		return
	}

	telegramToken := getenv("TELEGRAM_TOKEN", "")
	if telegramToken == "" {
		log.Error("Telegram token not set")
	}

	telegramChat := getenv("TELEGRAM_CHAT", "")
	if telegramChat == "" {
		log.Error("Telegram Chat ID not set")
	}
	telegramChatID, _ := strconv.ParseInt(telegramChat, 10, 64)

	// ? Init telegram bot
	bot, err := tgbotapi.NewBotAPI(telegramToken)
	if err != nil {
		log.Panicf("Bot can't connect , %v", err)
	}
	bot.Debug = true

	// ? Create http server
	router := mux.NewRouter()

	router.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": Version})
	}).Methods("GET")

	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "OK")
	}).Methods("GET")

	router.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		hook, _ := webhook.New(webhook.Options.Secret(secret))
		payload, err := hook.Parse(r, webhook.WatchEvent)
		if err != nil {
			if err == webhook.ErrEventNotFound {
				log.WithField("event", r.Header.Get("X-GitHub-Event")).Error(err)
			}
		}

		switch payload.(type) {

		case webhook.WatchPayload:
			log.Info("Is Watch request")
			watchRequest := payload.(webhook.WatchPayload)
			rawMsgS := fmt.Sprintf("New Github star for *%s* repo!. \n" +
				"The *%s* repo now has *%v* stars! 🎉. \n" +
				"Your new fan is %s", watchRequest.Repository.Name, watchRequest.Repository.Name, watchRequest.Repository.StargazersCount,watchRequest.Sender.HTMLURL)
			msg := tgbotapi.NewMessage(telegramChatID, rawMsgS)
			msg.ParseMode = "markdown"

			_, errBot := bot.Send(msg)
			if errBot != nil {
				log.Printf("Message can't send, %v", errBot)
			}
			_, _ = fmt.Fprint(w, "OK")

		}

		_, _ = fmt.Fprint(w, "Event received. Have a nice day")
	}).Methods("POST")

	// ? listen and serve on default 0.0.0.0:8080
	srv := &http.Server{
		Handler: tracing()(logging()(router)),
		Addr:    *listen + ":" + *port,
		// ! Good practice: enforce timeouts for servers you create!
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}
	log.Info("Serve at: ",*listen + ":" + *port)

	errServe := srv.ListenAndServe()
	if errServe != nil {
		log.Fatalf("Server err %s", errServe)
	}
}
type key int

const (
	requestIDKey key = 0
)

// ? logging logs http request with http details such as  header , userAgent
func logging() func(http.Handler) http.Handler {

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			requestID, ok := r.Context().Value(requestIDKey).(string)
			if !ok {
				requestID = "unknown"
			}

			hostname, err := os.Hostname()
			if err != nil {
				hostname = "unknow"
			}

			start := time.Now()

			// ? Execute next htpp middleware and calculate execution time
			next.ServeHTTP(w, r)

			stop := time.Since(start)
			latency := int(math.Ceil(float64(stop.Nanoseconds()) / 1000000.0))

			// ? Try to get user ip
			IPAddress := r.Header.Get("X-Real-Ip")
			if IPAddress == "" {
				IPAddress = r.Header.Get("X-Forwarded-For")
			}
			if IPAddress == "" {
				IPAddress = r.RemoteAddr
			}

			log.WithFields(log.Fields{
				"hostname":  hostname,
				"requestID": requestID,
				"latency":   latency, // time to process
				"clientIP":  IPAddress,
				"method":    r.Method,
				"path":      r.URL.Path,
				"header":    r.Header,
				"referer":   r.Referer(),
				"userAgent": r.UserAgent(),
			}).Info("Request")
		})
	}
}

// ? tracing trace http request with "X-Request-Id"
func tracing() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get("X-Request-Id")
			if requestID == "" {
				requestID = strconv.FormatInt(time.Now().UnixNano(), 10)
			}
			ctx := context.WithValue(r.Context(), requestIDKey, requestID)
			w.Header().Set("X-Request-Id", requestID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}
	return value
}