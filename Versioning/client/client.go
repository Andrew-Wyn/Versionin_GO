package main

import (
	"database/sql"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/andlabs/ui"
	_ "github.com/andlabs/ui/winmanifest"
	_ "github.com/mattn/go-sqlite3"
)

var localpaths []string
var localtime []string
var status []int
var strsdb string
var mainwin *ui.Window

func synclocdir(db *sql.DB) {
	dir, err := os.Open("./shared/")
	if err != nil {
		return
	}
	defer dir.Close()

	fileInfos, err := dir.Readdir(-1)
	if err != nil {
		return
	}

	//per ogni file nella cartella "./shared" controllo la sua corrispondeza nel database sqlite e nel caso di mancata corrispondenza  lo aggiungo con tag di status 1(mai sincronizzato con il server)
	for _, fi := range fileInfos {
		check, _ := db.Query("SELECT * FROM files WHERE path = '" + fi.Name() + "'") // local database
		boole := true
		for check.Next() {
			boole = false
		}
		if boole {
			timen := time.Now().Format("2006-01-02 15:04:05")
			statement, _ := db.Prepare("INSERT INTO files (path, time) VALUES (?, ?)")
			statement.Exec(fi.Name(), timen)

			localpaths = append(localpaths, fi.Name())
			localtime = append(localtime, timen)
			status = append(status, 1)
		}
	}
}

//controllo ogni file remoto e lo confronto con il corrispettivo file locale se esiste lo aggiorno o lo sincronizzo, se non esiste ne creo una copia e lo aggiongo al database locale !!!!TO FIX!!!!
func syncall(db *sql.DB, sdb *sql.DB, model *ui.TableModel) {
	rows, err := sdb.Query("SELECT * FROM files")

	uptime := time.Now()

	if err != nil {
		panic(err)
	}

	var (
		scaricati int // variabili intere gia inizializzate con zero non NIL
		caricati  int
		nuovi     int
	)

	var path string
	var stime time.Time
	var scontent []byte
	for rows.Next() {
		rows.Scan(&path, &stime, &scontent)
		var boole bool
		boole = true
		check, _ := db.Query("SELECT time FROM files WHERE path = '" + path + "'") // local database
		for check.Next() {
			var loctime time.Time
			check.Scan(&loctime)
			//controllare confroto date
			fmt.Println(loctime) //data ritorna sbagliata
			fmt.Println(stime)

			if loctime.Sub(stime) >= 0 {

				fmt.Println("challenge to server")

				//load blob file in a sql database
				content, err := ioutil.ReadFile("./shared/" + path)
				if err != nil {
					panic(err)
				}

				statement, err := sdb.Prepare("UPDATE files SET time = ?, content = ? WHERE path = ?")

				res, err := statement.Exec(uptime.Format("2006-01-02 15:04:05"), content, path)
				if err != nil {
					panic(err)
				}
				a, err := res.LastInsertId()
				if err != nil {
					panic(err)
				}
				b, err := res.RowsAffected()
				if err != nil {
					panic(err)
				}
				log.Printf("ID = %d, affected = %d\n", a, b)

				updatelocaltime(path, uptime, db)

				localtime[getlocalpatharraypos(localpaths, path)] = uptime.Format("2006-01-02 15:04:05")

				status[getlocalpatharraypos(localpaths, path)] = 0

				caricati++

				boole = false
			} else {

				fmt.Println("challenge from server")

				var _, err = os.Stat("./shared/" + path)

				// create file if not exists
				if os.IsNotExist(err) {
					var file, _ = os.Create("./shared/" + path)
					defer file.Close()
				}

				err = ioutil.WriteFile("./shared/"+path, scontent, 0644)
				if err != nil {
					log.Fatal(err)
				}

				updatelocaltime(path, stime, db)

				localtime[getlocalpatharraypos(localpaths, path)] = stime.Format("2006-01-02 15:04:05")
				status[getlocalpatharraypos(localpaths, path)] = -1

				scaricati++

				boole = false
			}
		}
		if boole {
			fmt.Println("new file from server")

			// detect if file exist
			filenametemp := "./shared/" + strings.Join(strings.Fields(path), "")

			var file, err = os.Create(filenametemp)
			if err != nil {
				panic(err)
			}
			defer file.Close()

			err = ioutil.WriteFile(filenametemp, scontent, 0644)
			if err != nil {
				panic(err)
			}

			statement, _ := db.Prepare("INSERT INTO files (path, time) VALUES (?, ?)")
			statement.Exec(path, stime)

			model.RowInserted(len(localpaths)) //notificare prima della modifica degli array che indicano il numero delle righe

			localpaths = append(localpaths, path)
			localtime = append(localtime, stime.String())
			status = append(status, -1)

			nuovi++
		}
	}

	rows, err = db.Query("SELECT * FROM files")

	if err != nil {
		panic(err)
	}

	var ltime time.Time
	path = strings.Join(strings.Fields(path), "")
	for rows.Next() {
		rows.Scan(&path, &ltime)
		check, _ := sdb.Query("SELECT * FROM files WHERE path = '" + path + "'") // db online
		stchecked := false
		for check.Next() {
			stchecked = true
		}
		if !stchecked {
			status[getlocalpatharraypos(localpaths, path)] = 1 //file locale non sincronizzato con il database
		}
	}

	var output string

	if scaricati != 0 {
		output += "aggiornati " + strconv.Itoa(scaricati) + "files, dal server \n"
	}
	if caricati != 0 {
		output += "caricati " + strconv.Itoa(caricati) + "files, sul server \n"
	}
	if nuovi != 0 {
		output += "aggiunti " + strconv.Itoa(nuovi) + "files, dal server \n"
	}

	ui.MsgBox(mainwin,
		"Sync Successfull Done, Sono stati:",
		output)
}

func getlocalpatharraypos(localpaths []string, value string) int {
	for p, v := range localpaths {
		if v == value {
			return p
		}
	}
	return -1
}

func updatelocaltime(path string, timep time.Time, db *sql.DB) {
	timen := timep.Format("2006-01-02 15:04:05")
	//aggiorno il tempo locale se faccio il download o upload
	statement2, _ := db.Prepare("update files set time = ? where path = ?")
	statement2.Exec(timen, path)
}

//inizializzo i vettori paralleli che contengo le informazioni comprensibili dal modello della tabella, inoltre assegno lo status se esistono o meno sul server
func initPaths(db *sql.DB, sdb *sql.DB) {
	defer func() {
		rec := recover()
		if rec != nil {
			statement, _ := db.Prepare("CREATE TABLE IF NOT EXISTS files (path TEXT PRIMARY KEY, time DATETIME)")
			statement.Exec()
		}
	}()

	rows, err := db.Query("SELECT * FROM files")

	if err != nil {
		panic(err)
	}

	var path string
	var ltime time.Time
	path = strings.Join(strings.Fields(path), "")
	for rows.Next() {
		rows.Scan(&path, &ltime)
		localtime = append(localtime, ltime.Format("2006-01-02 15:04:05"))
		localpaths = append(localpaths, path)
		check, _ := sdb.Query("SELECT * FROM files WHERE path = '" + path + "'") // db online
		stchecked := false
		for check.Next() {
			status = append(status, 0) //file locale sincronizzato con il database
			stchecked = true
		}
		if !stchecked {
			status = append(status, 1) //file locale non sincronizzato con il database
		}
	}
}

// CopyFile copies a file from src to dst. If src and dst files exist, and are
// the same, then return success. Otherise, attempt to create a hard link
// between the two files. If that fail, copy the file contents from src to dst.
func CopyFile(src, dst string) (err error) {
	sfi, err := os.Stat(src)
	if err != nil {
		return
	}
	if !sfi.Mode().IsRegular() {
		// cannot copy non-regular files (e.g., directories,
		// symlinks, devices, etc.)
		return fmt.Errorf("CopyFile: non-regular source file %s (%q)", sfi.Name(), sfi.Mode().String())
	}
	dfi, err := os.Stat(dst)
	if err != nil {
		if !os.IsNotExist(err) {
			return
		}
	} else {
		if !(dfi.Mode().IsRegular()) {
			return fmt.Errorf("CopyFile: non-regular destination file %s (%q)", dfi.Name(), dfi.Mode().String())
		}
		if os.SameFile(sfi, dfi) {
			return
		}
	}
	if err = os.Link(src, dst); err == nil {
		return
	}
	err = copyFileContents(src, dst)
	return
}

// copyFileContents copies the contents of the file named src to the file named
// by dst. The file will be created if it does not already exist. If the
// destination file exists, all it's contents will be replaced by the contents
// of the source file.
func copyFileContents(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return
	}
	err = out.Sync()
	return
}

type modelHandler struct {
}

func newModelHandler() *modelHandler {
	m := new(modelHandler)
	return m
}

func (mh *modelHandler) ColumnTypes(m *ui.TableModel) []ui.TableValue {
	return []ui.TableValue{
		ui.TableString(""), // column 0 text
		ui.TableString(""), // column 1 text
		ui.TableString(""), // column 2 button text
	}
}

func (mh *modelHandler) NumRows(m *ui.TableModel) int {
	return len(localpaths)
}

func (mh *modelHandler) CellValue(m *ui.TableModel, row, column int) ui.TableValue {
	if column == 3 {
		if status[row] == 1 {
			return ui.TableColor{1, 1, 0, 1}
		}
		if status[row] == -1 {
			return ui.TableColor{1, 0, 1, 1}
		}
		return nil
	}
	switch column {
	case 0:
		return ui.TableString(localpaths[row])
	case 1:
		return ui.TableString(localtime[row])
	case 2:
		return ui.TableString("Sync")
	}
	return nil
}

//onclick sui bottoni nelle celle della tabella
func (mh *modelHandler) SetCellValue(m *ui.TableModel, row, column int, value ui.TableValue) { //handle the click of a table buttons
	path := fmt.Sprintln(mh.CellValue(m, row, 0))
	//times := fmt.Sprintln(mh.CellValue(m, row, 1))

	fmt.Println(column)

	path = strings.Join(strings.Fields(path), "")

	//layout := "2014-09-12T11:45:26.371Z"

	localdb, _ := sql.Open("sqlite3", "./versioning.db?parseTime=true")

	db, err := sql.Open("mysql", strsdb) //connect to db "144.developer.master@gmail.com:Karate1998@tcp(versioning2.caspio.com:3306)/c0dcg029?parseTime=true" ??
	if err != nil {
		panic(err)
	}

	rows, err := db.Query("SELECT * FROM files WHERE path = '" + path + "'")
	if err != nil {
		panic(err)
	}

	for rows.Next() { //to fetch the result set

		fmt.Println("checked")

		var path string
		var stime time.Time
		var scontent []byte
		err = rows.Scan(&path, &stime, &scontent)
		if err != nil {
			panic(err)
		}

		path = strings.Join(strings.Fields(path), "")

		//controlli di modifica
		rows, _ := localdb.Query("SELECT time FROM files WHERE path = '" + path + "'")
		var loctime time.Time
		for rows.Next() {
			rows.Scan(&loctime)
		}

		fmt.Println()
		fmt.Println(stime)
		fmt.Println()
		fmt.Println(loctime)

		if loctime.Sub(stime) >= 0 {

			fmt.Println("challenge to server")

			//load blob file in a sql database
			content, err := ioutil.ReadFile("./shared/" + path)
			if err != nil {
				panic(err)
			}

			uptime := time.Now()

			statement, err := db.Prepare("UPDATE files SET time = ?, content = ? WHERE path = ?")

			res, err := statement.Exec(uptime.Format("2006-01-02 15:04:05"), content, path)
			if err != nil {
				log.Fatal(err)
			}
			a, err := res.LastInsertId()
			if err != nil {
				log.Fatal(err)
			}
			b, err := res.RowsAffected()
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("ID = %d, affected = %d\n", a, b)

			updatelocaltime(path, uptime, localdb)

			localtime[row] = uptime.Format("2006-01-02 15:04:05")

			status[row] = 0

			ui.MsgBox(mainwin,
				"Transition Successfull Done -UPLOAD-",
				"il file è stato caricato e aggiornato sul server")

			return
		} else {

			fmt.Println("challenge from server")

			err := ioutil.WriteFile("./shared/"+path, scontent, 0644)
			if err != nil {
				log.Fatal(err)
			}

			updatelocaltime(path, stime, localdb)

			localtime[row] = stime.Format("2006-01-02 15:04:05")
			status[row] = -1

			ui.MsgBox(mainwin,
				"Transition Successfull Done -DOWNLOAD-",
				"il file è stato scaricato e aggiornato dal server")

			return
		}
	}

	fmt.Println("--LOAD--")

	//load blob file in a sql database
	content, err := ioutil.ReadFile("./shared/" + path)
	if err != nil {
		log.Fatal(err)
	}

	statement, _ := db.Prepare("INSERT INTO files (path, time, content) VALUES (?,?,?);")
	statement.Exec(path, time.Now().Format("2006-01-02 15:04:05"), content)

	updatelocaltime(path, time.Now(), localdb)

	localtime[row] = time.Now().Format("2006-01-02 15:04:05")

	status[row] = 0

	ui.MsgBox(mainwin,
		"Transition Successfull Done -UPLOAD-",
		"il file è stato caricato per la prima volta")

}

func main() {

	strsdb = "versioning:versioning@tcp(db4free.net:3306)/versioning?parseTime=true"

	database, err := sql.Open("sqlite3", "./versioning.db?parseTime=true")
	if err != nil {
		panic(err)
	}
	sdb, err := sql.Open("mysql", strsdb) //connect to db
	if err != nil {
		panic(err)
	}

	var cartella string

	err = ui.Main(func() {

		synclocdir(database)
		initPaths(database, sdb)

		//dichiarazione oggetti grafici
		fileChooser := ui.NewButton("Open File")
		btnAdd := ui.NewButton("Add")
		txtPath := ui.NewEntry()
		btnSync := ui.NewButton("Sync All from SERVER")
		mh := newModelHandler()
		model := ui.NewTableModel(mh)
		table := ui.NewTable(&ui.TableParams{
			Model: model,
			RowBackgroundColorModelColumn: 3,
		})

		panel1 := ui.NewHorizontalBox()
		panel2 := ui.NewHorizontalBox()
		panel3 := ui.NewHorizontalBox()

		box := ui.NewVerticalBox()
		/******************************************/

		txtPath.SetReadOnly(true)

		//inserimento oggetti grafici nel panel principale
		panel1.Append(fileChooser, true)
		panel1.Append(txtPath, false)
		panel1.Append(btnAdd, true)
		panel2.Append(btnSync, true)
		panel3.Append(table, true)
		box.Append(panel1, false)
		box.Append(panel2, false)
		box.Append(panel3, true)
		/******************************************/

		table.AppendTextColumn("File",
			0, ui.TableModelColumnNeverEditable, nil)
		table.AppendTextColumn("Time",
			1, ui.TableModelColumnNeverEditable, nil)
		table.AppendButtonColumn("Sync",
			2, ui.TableModelColumnAlwaysEditable)

		//dichiarazione della finestra prinicipale dell' applicazione
		window := ui.NewWindow("Versioning", 500, 500, true)
		mainwin = window

		window.SetMargined(true)

		window.SetChild(box)
		/******************************************/

		//dichiarazione dei listener

		fileChooser.OnClicked(func(*ui.Button) {
			filename := ui.OpenFile(window)
			if filename == "" {
				filename = "(cancelled)"
			}
			txtPath.SetText(filename)
		})

		btnAdd.OnClicked(func(*ui.Button) {

			if cartella == "" {
				CopyFile(txtPath.Text(), "./shared/"+filepath.Base(txtPath.Text()))
			}

			timen := time.Now().Format("2006-01-02 15:04:05")

			fmt.Println(timen)

			statement, _ := database.Prepare("CREATE TABLE IF NOT EXISTS files (path TEXT PRIMARY KEY, time DATETIME)")
			statement.Exec()
			statement, _ = database.Prepare("INSERT INTO files (path, time) VALUES (?, ?)")
			statement.Exec(filepath.Base(txtPath.Text()), timen)

			model.RowInserted(len(localpaths)) //notificare prima della modifica degli array che indicano il numero delle righe

			localpaths = append(localpaths, filepath.Base(txtPath.Text()))
			localtime = append(localtime, timen)
			status = append(status, 1)
		})

		btnSync.OnClicked(func(*ui.Button) {
			syncall(database, sdb, model)
		})

		window.OnClosing(func(*ui.Window) bool {

			ui.Quit()

			return true

		})
		/******************************************/

		//rendere visibile il contenitore principale dell applicazione
		window.Show()
		/******************************************/

	})

	if err != nil {

		//panic(err)

	}

}
