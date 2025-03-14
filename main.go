package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/url"
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

const (
	telegramBotToken = "8052743293:AAGQTDeTPKBNtRklb1aVEFT_QYDJKBT_g8g"
	telegramChatID   = "-1002433576509"
	telegramThreadID = "875"
	dbFile           = "cursos.db"
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
	_, err = db.Exec(query)
	if err != nil {
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
	_, err := db.Exec("INSERT INTO cursos (url, titulo, lugar, periodo, hora, plazas, costo) VALUES (?, ?, ?, ?, ?, ?, ?)",
		curso.URL, curso.Titulo, curso.Lugar, curso.Periodo, curso.Hora, curso.Plazas, curso.Costo)
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
	params.Set("message_thread_id", telegramThreadID)

	_, err := http.PostForm(telegramAPI, params)
	if err != nil {
		log.Println("Error enviando mensaje a Telegram:", err)
	}
}


func main() {
	db := initDB()
	defer db.Close()
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
			Titulo: e.ChildText("span.convocatoria-titulo"),
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

	err := c.Visit("https://formacionagraria.tenerife.es/")
	if err != nil {
		log.Fatal(err)
	}

	c.Wait()

	fmt.Println("\nCursos encontrados:", len(cursos))
	var messageBuilder strings.Builder
	if len(cursos) > 0 {
            messageBuilder.WriteString(fmt.Sprintf("Hay cursos nuevos!\n"))
        }
	for i, curso := range cursos {
		fmt.Printf("\nCurso #%d:\n", i+1)
		fmt.Println("URL:", curso.URL)
		fmt.Println("Título:", curso.Titulo)
		fmt.Println("Lugar:", curso.Lugar)
		fmt.Println("Período:", curso.Periodo)
		fmt.Println("Horario:", curso.Hora)
		fmt.Println("Plazas:", curso.Plazas)
		fmt.Println("Costo:", curso.Costo)
		fmt.Println("-----------------------------------")
		messageBuilder.WriteString(fmt.Sprintf("\n*Curso %d:*\n", i+1))
		messageBuilder.WriteString(fmt.Sprintf("Título: %s\n", curso.Titulo))
		messageBuilder.WriteString(fmt.Sprintf("Lugar: %s\n", curso.Lugar))
		messageBuilder.WriteString(fmt.Sprintf("Período: %s\n", curso.Periodo))
		messageBuilder.WriteString(fmt.Sprintf("Horario: %s\n", curso.Hora))
		messageBuilder.WriteString(fmt.Sprintf("Plazas: %s\n", curso.Plazas))
		messageBuilder.WriteString(fmt.Sprintf("Costo: %s\n", curso.Costo))
		messageBuilder.WriteString(fmt.Sprintf("[Ver más](%s)\n", curso.URL))
		messageBuilder.WriteString("-----------------------------------\n")
	}
	sendTelegramMessage(messageBuilder.String())
}
