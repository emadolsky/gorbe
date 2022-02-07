package gorbe

import (
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/gobwas/glob"
	"gorbe/types"
	"log"
	"sync"
	"time"
)

const (
	backoffInterval     = 100 * time.Millisecond
	maximumBackoffCount = 6
)

type Gorbe struct {
	Machine        types.Machine
	userStateStore types.UserStateStore
	workerCount    int

	bot *tgbotapi.BotAPI

	stateRoutinesByName        map[string]types.StateRoutineFunc
	transitionValidationByName map[string]types.TransitionValidationFunc
	transitionActionByName     map[string]types.TransitionActionFunc

	close chan struct{}
}

func NewGorbeWithConfig(bot *tgbotapi.BotAPI, config *types.Config, userStore types.UserStateStore) *Gorbe {
	if userStore == nil {
		userStore = NewInMemoryStore()
	}
	return &Gorbe{
		Machine:        config.Machine,
		userStateStore: userStore,
		workerCount:    config.WorkerCount,

		bot: bot,

		stateRoutinesByName:        make(map[string]types.StateRoutineFunc),
		transitionValidationByName: make(map[string]types.TransitionValidationFunc),
		transitionActionByName:     make(map[string]types.TransitionActionFunc),

		close: make(chan struct{}),
	}
}

func NewGorbe(bot *tgbotapi.BotAPI, configPath string, store types.UserStateStore) (*Gorbe, error) {
	cfg, err := types.ReadConfig(configPath)
	if err != nil {
		return nil, err
	}
	return NewGorbeWithConfig(bot, cfg, store), nil
}

func (g *Gorbe) RegisterStateRoutine(name string, fn types.StateRoutineFunc) {
	g.stateRoutinesByName[name] = fn
	for i := range g.Machine.States {
		log.Println(g.Machine.States[i].StateRoutine)
		if g.Machine.States[i].StateRoutine == name {
			log.Println(name)
			g.Machine.States[i].StateFunc = fn
		}
	}
}

func (g *Gorbe) RegisterTransitionValidation(name string, fn types.TransitionValidationFunc) {
	g.transitionValidationByName[name] = fn
	for _, s := range g.Machine.States {
		for _, t := range s.Transitions {
			if t.ValidationRoutine == name {
				t.ValidationFunc = fn
			}
		}
	}
}

func (g *Gorbe) RegisterTransitionAction(name string, fn types.TransitionActionFunc) {
	g.transitionActionByName[name] = fn
	for _, s := range g.Machine.States {
		for _, t := range s.Transitions {
			if t.ActionRoutine == name {
				t.ActionFunc = fn
			}
		}
	}
}

func (g *Gorbe) getStateByName(name string) *types.State {
	// TODO: easily optimize with map
	for _, s := range g.Machine.States {
		if s.Name == name {
			return &s
		}
	}
	return nil
}

func (g *Gorbe) Run() error {
	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 60

	updateChan := g.bot.GetUpdatesChan(updateConfig)
	retryChan := make(chan updateRetry, g.bot.Buffer)

	workerWG := &sync.WaitGroup{}
	for i := 0; i < g.workerCount; i++ {
		workerWG.Add(1)
		go g.runWorker(workerWG, updateChan, retryChan)
	}
	workerWG.Wait()
	return nil
}

type updateRetry struct {
	err     error
	update  tgbotapi.Update
	backoff int
}

func (g *Gorbe) runWorker(wg *sync.WaitGroup, updateCh tgbotapi.UpdatesChannel, retryCh chan updateRetry) {
	defer func() {
		wg.Done()
	}()
	for {
		select {
		case <-g.close:
			return
		case update := <-updateCh:
			err := g.dispatchUpdate(update)
			if err != nil {
				time.AfterFunc(backoffInterval, func() {
					retryCh <- updateRetry{
						update:  update,
						err:     err,
						backoff: 1,
					}
				})
			}
		case retry := <-retryCh:
			err := g.dispatchUpdate(retry.update)
			if err != nil {
				if retry.backoff > maximumBackoffCount {
					log.Printf("dropping update: %d\n", retry.update.UpdateID)
					continue
				}
				backoffDuration := backoffInterval * time.Duration(2<<retry.backoff)
				time.AfterFunc(backoffDuration, func() {
					retryCh <- updateRetry{
						update:  retry.update,
						err:     err,
						backoff: retry.backoff + 1,
					}
				})
			}
		}
	}
}

func (g *Gorbe) dispatchUpdate(update tgbotapi.Update) error {
	chat := update.FromChat()
	if chat == nil {
		log.Printf("could not get chat of update. ignoring: %d\n", update.UpdateID)
		return nil
	}

	chatId := chat.ID
	stateName, err := g.userStateStore.GetUserState(chatId)
	if err != nil {
		return err
	}
	state := g.getStateByName(stateName)
	var nextStateName string

	if state == nil {
		if isStartUpdate(update) {
			state = g.getStateByName(g.Machine.StartStateName)
		} else {
			return types.NewStateNotFoundError(stateName)
		}
	}
	transition := g.matchTransition(update, state)

	if transition == nil {
		log.Printf("invalid transition: %d\n", update.UpdateID)
		// TODO: SHOULD SHOW SOME BULLSHIT
		return nil
	}

	if err := g.performTransitionValidityCheck(update, transition); err != nil {
		return err
	}

	nextStateName = transition.NextState
	if err := g.userStateStore.SetUserState(chatId, nextStateName); err != nil {
		return err
	}

	state = g.getStateByName(nextStateName)
	if state == nil {
		return types.NewStateNotFoundError(stateName)
	}

	if err := g.sendDefaultStateMessage(update, state); err != nil {
		return err
	}

	if state.StateFunc != nil {
		return state.StateFunc(g.bot, *chat)
	}
	return nil
}

func (g *Gorbe) matchTransition(update tgbotapi.Update, state *types.State) *types.StateTransition {
	if isStartUpdate(update) {
		return g.getNoOpTransition(state)
	}
	var updateCommandType types.TransitionCommandType
	switch {
	case update.CallbackQuery != nil:
		updateCommandType = types.TransitionCommandTypeCallback
	case update.Message != nil:
		updateCommandType = types.TransitionCommandTypeMessage
	}
	for _, t := range state.Transitions {
		if t.Command.Type != updateCommandType {
			continue
		}
		if t.Command.Type == types.TransitionCommandTypeMessage {
			c, err := glob.Compile(t.Command.Match)
			if err != nil {
				log.Printf("command matcher error: %s\n", err)
				continue
			}
			if !c.Match(update.Message.Text) {
				continue
			}
		}
		return &t
	}
	return nil
}

func (g *Gorbe) getNoOpTransition(state *types.State) *types.StateTransition {
	stateName := ""
	if state != nil {
		stateName = state.Name
	}
	return &types.StateTransition{
		Command: types.TransitionCommand{
			Name: "noop",
		},
		NextState: stateName,
	}
}

func (g *Gorbe) performTransitionValidityCheck(update tgbotapi.Update, transition *types.StateTransition) error {
	if transition.ValidationFunc == nil {
		return nil
	}
	if isValid, reason, err := transition.ValidationFunc(g.bot, update); err != nil {
		return err
	} else if !isValid {
		chatId := update.FromChat().ID
		msg := tgbotapi.NewMessage(chatId, fmt.Sprintf("error: %s", reason))
		_, err := g.bot.Send(msg)
		return err
	}

	return transition.ActionFunc(g.bot, update)
}

func (g *Gorbe) sendDefaultStateMessage(update tgbotapi.Update, state *types.State) error {
	chatId := update.FromChat().ID
	kbMarkup := g.createStateReplyKeyboard(state)
	msg := tgbotapi.NewMessage(chatId, state.Name)
	if len(kbMarkup.Keyboard) == 0 {
		msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(false)
	} else {
		msg.ReplyMarkup = kbMarkup
	}
	_, err := g.bot.Send(msg)
	return err
}

func (g *Gorbe) createStateReplyKeyboard(state *types.State) tgbotapi.ReplyKeyboardMarkup {
	kbMarkup := tgbotapi.ReplyKeyboardMarkup{
		Keyboard:       nil,
		ResizeKeyboard: true,
	}
	if state.KeyboardMarkup != nil {
		kbMarkup.Keyboard = make([][]tgbotapi.KeyboardButton, len(state.KeyboardMarkup.Keyboard))
		copy(kbMarkup.Keyboard, state.KeyboardMarkup.Keyboard)
	}
	var transitionKeyboards []tgbotapi.KeyboardButton
	for _, t := range state.Transitions {
		if t.Command.KeyboardMarkup != "" {
			transitionKeyboards = append(transitionKeyboards, tgbotapi.KeyboardButton{
				Text: t.Command.KeyboardMarkup,
			})
		}
	}
	if len(transitionKeyboards) != 0 {
		transitionKeyboardRows := make([][]tgbotapi.KeyboardButton, (len(transitionKeyboards)-1)/3+1)
		for i, tk := range transitionKeyboards {
			transitionKeyboardRows[i/3] = append(transitionKeyboardRows[i/3], tk)
		}
		kbMarkup.Keyboard = append(kbMarkup.Keyboard, transitionKeyboardRows...)
	}
	return kbMarkup
}
