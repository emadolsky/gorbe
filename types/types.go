package types

import (
	"encoding/json"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"os"
)

type State struct {
	Name           string                        `json:"name"`
	KeyboardMarkup *tgbotapi.ReplyKeyboardMarkup `json:"keyboardMarkup,omitempty"`
	Transitions    []StateTransition             `json:"transitions"`
	StateRoutine   string                        `json:"stateRoutine"`
	StateFunc      StateRoutineFunc
}

type StateTransition struct {
	Command           TransitionCommand `json:"command"`
	Params            []ActionParam     `json:"params"`
	ValidationRoutine string            `json:"validationRoutine"`
	NextState         string            `json:"nextState"`
	ActionRoutine     string            `json:"actionRoutine"`
	ValidationFunc    TransitionValidationFunc
	ActionFunc        TransitionActionFunc
}

type TransitionCommand struct {
	Name           string                `json:"name"`
	Type           TransitionCommandType `json:"type"`
	KeyboardMarkup string                `json:"keyboardMarkup"`
	Match          string                `json:"match"`
}

type TransitionCommandType string

const (
	TransitionCommandTypeMessage  TransitionCommandType = "message"
	TransitionCommandTypeCallback TransitionCommandType = "callback"
)

type StateRoutineFunc func(*tgbotapi.BotAPI, tgbotapi.Chat) (err error)
type TransitionValidationFunc func(*tgbotapi.BotAPI, tgbotapi.Update) (isValid bool, reason string, err error)
type TransitionActionFunc func(*tgbotapi.BotAPI, tgbotapi.Update) (err error)

type ActionParam struct {
	Name string          `json:"name"`
	Type ActionParamType `json:"type"`
}

type ActionParamType string

const (
	ActionParamTypeString ActionParamType = "string"
	ActionParamTypeNumber ActionParamType = "number"
)

type Config struct {
	Machine     Machine `json:"machine"`
	WorkerCount int     `json:"workerCount"`
}

type Machine struct {
	States         []State `json:"states"`
	StartStateName string  `json:"startState"`
}

func ReadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	jd := json.NewDecoder(file)
	conf := &Config{}
	err = jd.Decode(conf)
	return conf, err
}

type UserStateStore interface {
	GetUserState(chatId int64) (string, error)
	SetUserState(chatId int64, state string) error
}
