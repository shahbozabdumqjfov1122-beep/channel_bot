package main

import (
	"fmt"
	"html"
	"log"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	bot, err := tgbotapi.NewBotAPI("TOKENINGIZNI_BU_YERGA_QOYING")
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	// Admin ID va username
	adminID := int64(6649453730)
	adminUsername := "TM_ESPORTS"

	// Admin qo‘shgan kanallar
	channels := make(map[int64]string)

	// Oxirgi foydalanuvchi
	var lastUserID int64
	var lastUserName string

	// 3 daqiqa log tekshiruv
	go func() {
		for {
			time.Sleep(3 * time.Minute)
			if lastUserID != 0 {
				log.Printf("3 daqiqa o‘tdi. Oxirgi yozgan: %s (%d)", lastUserName, lastUserID)
			}
		}
	}()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	// Admin panel tugmalari
	adminMenu := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Kanal qo‘shish", "add"),
			tgbotapi.NewInlineKeyboardButtonData("➖ Kanal o‘chirish", "remove"),
		),
	)

	var waitingChannelName bool
	var waitingChannelID bool
	var tempChannelName string

	for update := range updates {

		// CALLBACK HANDLER
		if update.CallbackQuery != nil {

			data := update.CallbackQuery.Data

			if data == "add" {
				waitingChannelName = true
				bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "📌 Kanal nomini yuboring:"))
				continue
			}

			if data == "remove" {
				if len(channels) == 0 {
					bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "❌ Hali kanal yo‘q"))
					continue
				}

				var rows [][]tgbotapi.InlineKeyboardButton
				for id, name := range channels {
					btn := tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%s (%d)", name, id), "del_"+strconv.FormatInt(id, 10))
					rows = append(rows, []tgbotapi.InlineKeyboardButton{btn})
				}
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("🛠 Admin Panel — @%s", adminUsername))
				msg.ReplyMarkup = adminMenu
				bot.Send(msg)
			}

			if len(data) > 4 && data[:4] == "del_" {
				idStr := data[4:]
				id, _ := strconv.ParseInt(idStr, 10, 64)
				delete(channels, id)
				bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "✅ Kanal o‘chirildi"))
				continue
			}
		}

		// TEXT HANDLER
		if update.Message != nil {

			// /admin buyruq
			if update.Message.Text == "/admin" {
				if update.Message.From.ID != adminID {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ Siz admin emassiz!"))
					continue
				}

				msgText := fmt.Sprintf("🛠 Admin Panel — @%s", adminUsername)
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, msgText)
				msg.ReplyMarkup = adminMenu
				bot.Send(msg)
				continue
			}

			// Kanal qo‘shish jarayoni
			if update.Message.From.ID == adminID {
				if waitingChannelName {
					tempChannelName = update.Message.Text
					waitingChannelName = false
					waitingChannelID = true
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "📌 Kanal ID sini yuboring (-100...)"))
					continue
				}

				if waitingChannelID {
					id, err := strconv.ParseInt(update.Message.Text, 10, 64)
					if err != nil {
						bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ ID xato!"))
						continue
					}
					channels[id] = tempChannelName
					waitingChannelID = false
					tempChannelName = ""
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "✅ Kanal qo‘shildi"))
					continue
				}
			}

			// Xush kelibsiz funksiyasi
			chatID := update.Message.Chat.ID
			if _, ok := channels[chatID]; ok {

				if len(update.Message.NewChatMembers) > 0 {
					for _, user := range update.Message.NewChatMembers {
						if user.IsBot {
							continue
						}

						var msg tgbotapi.MessageConfig
						if user.UserName != "" {
							msg = tgbotapi.NewMessage(chatID, "Salom @"+user.UserName+"! Xush kelibsiz 🎉")
						} else {
							name := html.EscapeString(user.FirstName)
							txt := fmt.Sprintf("Salom <a href=\"tg://user?id=%d\">%s</a>! Xush kelibsiz 🎉", user.ID, name)
							msg = tgbotapi.NewMessage(chatID, txt)
							msg.ParseMode = "HTML"
						}

						bot.Send(msg)
					}
				}

				// Oxirgi foydalanuvchini eslab qolish
				if !update.Message.From.IsBot {
					lastUserID = update.Message.From.ID
					//lastMessageID = update.Message.MessageID
					lastUserName = update.Message.From.FirstName
				}
			}
		}
	}
}
