package main

import (
	"gorbe/gorbe"
	"log"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	bot, err := tgbotapi.NewBotAPI(os.Getenv("TOKEN"))
	if err != nil {
		log.Panic(err)
	}
	bot.Debug = true

	pishi, err := gorbe.NewGorbe(bot, "./config.json", nil)
	if err != nil {
		log.Panic(err)
	}

	pishi.RegisterStateRoutine("showChat", func(bot *tgbotapi.BotAPI, chat tgbotapi.Chat) (err error) {
		msg := tgbotapi.NewMessage(chat.ID, chat.UserName)
		_, err = bot.Send(msg)
		return err
	})

	err = pishi.Run()
	log.Fatalln(err)
}
