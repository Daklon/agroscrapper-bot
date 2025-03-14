package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gocolly/colly"
	_ "github.com/mattn/go-sqlite3"
)

type Curso struct {
	URL      string
	Titulo   string
	Lugar    string
	Periodo  string
	Hora     string
	Plazas   string
	Costo    string
}

var (
	telegramBotToken string
	telegramChatID   string
	telegramThreadID string
	dbFile           string
)

func initDB() *sql.DB {
	db, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		log.Fatal(err)
	}

	query := `CREATE TABLE IF NOT EXISTS cursos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT UNIQUE,
		titulo TEXT,
		lugar TEXT,
		periodo TEXT,
		hora TEXT,
		plazas TEXT,
		costo TEXT
	)`

	if _, err = db.Exec(query); err != nil {
		log.Fatal(err)
	}
	return db
}

func cursoExiste(db *sql.DB, url string) bool {
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM cursos WHERE url=?)", url).Scan(&exists)
	if err != nil && err != sql.ErrNoRows {
		log.Fatal(err)
	}
	return exists
}

func guardarCurso(db *sql.DB, curso Curso) {
	_, err := db.Exec(
		"INSERT INTO cursos (url, titulo, lugar, periodo, hora, plazas, costo) VALUES (?, ?, ?, ?, ?, ?, ?)",
		curso.URL, curso.Titulo, curso.Lugar, curso.Periodo, curso.Hora, curso.Plazas, curso.Costo,
	)
	if err != nil {
		log.Println("Error guardando en la base de datos:", err)
	}
}

func sendTelegramMessage(message string) {
	telegramAPI := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", telegramBotToken)
	params := url.Values{}
	params.Set("chat_id", telegramChatID)
	params.Set("text", message)
	params.Set("parse_mode", "Markdown")

	if telegramThreadID != "" {
		fmt.Println("threadId:", telegramThreadID)

		params.Set("message_thread_id", telegramThreadID)
	}

	_, err := http.PostForm(telegramAPI, params)
	if err != nil {
		log.Println("Error enviando mensaje a Telegram:", err)
	}
}

func main() {
	// Configuración de parámetros
	flag.StringVar(&dbFile, "db", "", "Ruta de la base de datos (env: DB_FILE)")
	flag.StringVar(&telegramBotToken, "token", "", "Token del bot de Telegram (env: TELEGRAM_TOKEN)")
	flag.StringVar(&telegramChatID, "chatid", "", "ID del chat de Telegram (env: TELEGRAM_CHATID)")
	flag.StringVar(&telegramThreadID, "threadid", "", "ID del hilo en Telegram (env: TELEGRAM_THREADID)")
	flag.Parse()

	// Obtener valores de entorno si no se establecieron por flags
	if dbFile == "" {
		dbFile = os.Getenv("DB_FILE")
		if dbFile == "" {
			dbFile = "cursos.db" // Valor por defecto original
		}
	}
	if telegramBotToken == "" {
		telegramBotToken = os.Getenv("TELEGRAM_TOKEN")
	}
	if telegramChatID == "" {
		telegramChatID = os.Getenv("TELEGRAM_CHATID")
	}
	if telegramThreadID == "" {
		telegramThreadID = os.Getenv("TELEGRAM_THREADID")
	}

	// Validar parámetros obligatorios
	if telegramBotToken == "" {
		log.Fatal("Se requiere el token de Telegram. Usa el flag -token o la variable TELEGRAM_TOKEN")
	}
	if telegramChatID == "" {
		log.Fatal("Se requiere el ID del chat. Usa el flag -chatid o la variable TELEGRAM_CHATID")
	}

	// Inicializar base de datos
	db := initDB()
	defer db.Close()

	// Configurar collector
	c := colly.NewCollector(
		colly.AllowedDomains("formacionagraria.tenerife.es"),
		colly.Async(true),
	)

	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: 3,
		Delay:       1 * time.Second,
	})

	var cursos []Curso

	// Encontrar enlaces a cursos en la página principal
	c.OnHTML("a[href^='/acfor-fo/actividades/']", func(e *colly.HTMLElement) {
		link := e.Attr("href")
		if strings.Contains(link, "solicitud") {
			return
		}
		if !strings.HasPrefix(link, "http") {
			link = e.Request.AbsoluteURL(link)
		}
		e.Request.Visit(link)
	})

	// Extraer detalles de cada curso
	c.OnHTML("div.container.page-body", func(e *colly.HTMLElement) {
		titulo := e.ChildText("span.convocatoria-titulo")
		if titulo == "" {
			return
		}

		curso := Curso{
			URL:    e.Request.URL.String(),
			Titulo: titulo,
		}

		// Buscar todos los campos en las filas
		e.ForEach("div.row", func(_ int, row *colly.HTMLElement) {
			label := row.ChildText("label")
			value := row.ChildText("div.col-xs-6.col-sm-8.col-md-10 span")

			switch strings.TrimSpace(label) {
			case "Lugar de impartición:":
				curso.Lugar = value
			case "Período de impartición:":
				curso.Periodo = value
			case "Horario de impartición:":
				curso.Hora = value
			case "Plazas disponibles:":
				curso.Plazas = value
			case "Importe:":
				curso.Costo = value
			}
		})

		if !cursoExiste(db, curso.URL) {
			cursos = append(cursos, curso)
			guardarCurso(db, curso)
		}
	})

	c.OnRequest(func(r *colly.Request) {
		fmt.Println("Visitando:", r.URL)
	})

	c.OnError(func(_ *colly.Response, err error) {
		log.Println("Error:", err)
	})

	if err := c.Visit("https://formacionagraria.tenerife.es/"); err != nil {
		log.Fatal(err)
	}

	c.Wait()

	// Construir y enviar mensaje
	var messageBuilder strings.Builder
	if len(cursos) > 0 {
		messageBuilder.WriteString("¡Hay cursos nuevos!\n\n")
		for i, curso := range cursos {
			fmt.Println("Preprando mensaje para curso:", curso.Titulo)
			messageBuilder.WriteString(
				fmt.Sprintf("*Curso %d:*\nTítulo: %s\nLugar: %s\nPeríodo: %s\nHorario: %s\nPlazas: %s\nCosto: %s\n[Ver más](%s)\n\n",
					i+1, curso.Titulo, curso.Lugar, curso.Periodo, curso.Hora, curso.Plazas, curso.Costo, curso.URL),
			)
		}
		sendTelegramMessage(messageBuilder.String())
		fmt.Println("mensaje enviado a:", telegramChatID)

	}
}
