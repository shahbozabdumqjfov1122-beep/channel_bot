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

	// Admin sozlamalari
	adminID := int64(6649453730)
	adminUsername := "TM_ESPORTS"

	// Kanal ro'yxati (map)
	channels := make(map[int64]string)

	// Oxirgi foydalanuvchi
	var lastUserID int64
	var lastUserName string

	// 3 daqiqa log
	go func() {
		for {
			time.Sleep(3 * time.Minute)
			if lastUserID != 0 {
				log.Printf("3 daqiqa o'tdi — Oxirgi yozgan: %s (%d)", lastUserName, lastUserID)
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

		// CALLBACK BOSILGANDA
		if update.CallbackQuery != nil {

			data := update.CallbackQuery.Data

			// Callbackga javob (loadingni o'chiradi)
			bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))

			// ➕ Kanal qo'shish bosilganda
			if data == "add" {
				waitingChannelName = true
				bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "📌 Kanal nomini yuboring:"))
				continue
			}

			// ➖ Kanal o'chirish bosilganda
			if data == "remove" {

				if len(channels) == 0 {
					bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "❌ Hali kanal yo‘q"))
					continue
				}

				var rows [][]tgbotapi.InlineKeyboardButton

				for id, name := range channels {
					btn := tgbotapi.NewInlineKeyboardButtonData(
						fmt.Sprintf("%s (%d)", name, id),
						"del_"+strconv.FormatInt(id, 10),
					)
					rows = append(rows, []tgbotapi.InlineKeyboardButton{btn})
				}

				msg := tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "Qaysi kanalni o‘chirasiz?")
				msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
				bot.Send(msg)
				continue
			}

			// delete tugmasi
			if len(data) > 4 && data[:4] == "del_" {
				idStr := data[4:]
				id, _ := strconv.ParseInt(idStr, 10, 64)
				delete(channels, id)
				bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "✅ Kanal o‘chirildi"))
				continue
			}
		}

		// TEXT XABARLAR
		if update.Message != nil {

			// /admin komandasi
			if update.Message.Text == "/admin" {
				if update.Message.From.ID != adminID {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ Siz admin emassiz!"))
					continue
				}

				msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("🛠 Admin Panel — @%s", adminUsername))
				msg.ReplyMarkup = adminMenu
				bot.Send(msg)
				continue
			}

			// Admin kanal qo‘shyapti
			if update.Message.From.ID == adminID {

				// 1) Kanal nomini kutyapmiz
				if waitingChannelName {
					tempChannelName = update.Message.Text
					waitingChannelName = false
					waitingChannelID = true
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "📌 Kanal ID sini yuboring (-100...)"))
					continue
				}

				// 2) Kanal ID ni kutyapmiz
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

			// HOZIRGI CHAT kanal ro‘yxatida bormi?
			chatID := update.Message.Chat.ID
			if _, ok := channels[chatID]; ok {

				// Yangi user qo'shilsa
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
					lastUserName = update.Message.From.FirstName
				}
			}
		}
	}
}
