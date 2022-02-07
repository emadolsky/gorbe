package gorbe

import tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

func isStartUpdate(update tgbotapi.Update) bool {
	return update.Message != nil && update.Message.Text == "/start"
}
