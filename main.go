package main

import (
	"fmt"
	"html"
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	const BOT_TOKEN = "8497820416:AAH6MELB1Egn4ORRxH2iITVty_nTvOltGJM"
	const ADMIN_USERNAME = "TM_ESPORTS"

	bot, err := tgbotapi.NewBotAPI(BOT_TOKEN)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true
	log.Printf("Bot authorized on account %s", bot.Self.UserName)

	// Kanal ro'yxati
	channels := make(map[int64]string)

	// Admin panel tugmalari
	adminMenu := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Kanal qo‘shish", "add"),
			tgbotapi.NewInlineKeyboardButtonData("➖ Kanal o‘chirish", "remove"),
		),
	)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for update := range updates {

		// CALLBACK BOSILGANDA
		if update.CallbackQuery != nil {

			data := update.CallbackQuery.Data
			bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "")) // loadingni o‘chiradi

			// ➕ Kanal qo‘shish bosilganda
			if data == "add" {
				msg := tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID,
					"📌 Kanal qo‘shish uchun shu formatda yozing:\n\nKanalNomi;-100123456789",
				)
				bot.Send(msg)
				continue
			}

			// ➖ Kanal o‘chirish bosilganda
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
			if strings.HasPrefix(data, "del_") {
				idStr := strings.TrimPrefix(data, "del_")
				id, _ := strconv.ParseInt(idStr, 10, 64)
				delete(channels, id)
				bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "✅ Kanal o‘chirildi"))
				continue
			}
		}

		// TEXT XABARLAR
		if update.Message != nil {

			// /start komandasi
			if update.Message.Text == "/start" {
				if update.Message.From.UserName != ADMIN_USERNAME {
					bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ Siz admin emassiz!"))
					continue
				}

				msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("🛠 Admin Panel — @%s", ADMIN_USERNAME))
				msg.ReplyMarkup = adminMenu
				bot.Send(msg)
				continue
			}

			// Admin kanal qo‘shishi — format: KanalNomi;-100123456789
			if update.Message.From.UserName == ADMIN_USERNAME {

				text := update.Message.Text

				if strings.Contains(text, ";") {
					parts := strings.SplitN(text, ";", 2)
					if len(parts) == 2 {

						name := parts[0]
						idStr := parts[1]

						id, err := strconv.ParseInt(idStr, 10, 64)
						if err != nil {
							bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, "❌ ID noto‘g‘ri!"))
							continue
						}

						channels[id] = name
						bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID,
							fmt.Sprintf("✅ Kanal qo‘shildi: %s (%d)", name, id)))
						continue
					}
				}
			}

			// Guruhga yangi odam kirsa — xush kelibsiz
			chatID := update.Message.Chat.ID
			if _, ok := channels[chatID]; ok {

				if len(update.Message.NewChatMembers) > 0 {

					for _, user := range update.Message.NewChatMembers {

						if user.IsBot {
							continue
						}

						var msg tgbotapi.MessageConfig

						if user.UserName != "" {
							msg = tgbotapi.NewMessage(chatID,
								"Salom @"+user.UserName+"! Xush kelibsiz 🎉")

						} else {
							name := html.EscapeString(user.FirstName)
							txt := fmt.Sprintf(
								"Salom <a href=\"tg://user?id=%d\">%s</a>! Xush kelibsiz 🎉",
								user.ID, name,
							)
							msg = tgbotapi.NewMessage(chatID, txt)
							msg.ParseMode = "HTML"
						}

						bot.Send(msg)
					}
				}
			}

		}
	}
}
