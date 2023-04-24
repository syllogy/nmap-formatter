package formatter

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	// Written this way to avoid automatic removal by text editor
	_ "github.com/mattn/go-sqlite3"
)

// SqliteFormatter is a main struct to handle output for Sqlite
type SqliteFormatter struct {
	config *Config
}

// Format the data to sqlite and output it to appropriate io.Writer
// If output is made to stdout and no additional options provided we simply
// print out sqlite raw binary data which then can be piped to the file.
// If OutputFile config is used, then we have no choice but to write down all data there
// In case if DSN option is provided, then we use DSN as a source of truth (OutputFile takes precedence
// if both are provided).
func (f *SqliteFormatter) Format(td *TemplateData, templateContent string) error {
	var err error

	// We have multiple tables that are joined together, firstly those are nmap runs, which have
	// hosts table and then the third one is ports table which is joined with the previous one,
	// probably there would be some kind of meta table with all other information about hosts/servers.
	// It's really hard to determine uniqueness of the scan, so we simply have to add new value to the table
	// and add columns which store the time when this scan was added

	db, err := sql.Open("sqlite3", f.config.OutputOptions.SqliteOutputOptions.DSN)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// The nf_ prefix in tables are related to nmap-formatter
	// either the creation date or passed options (identifier)
	// Identifiers are needed to help users to differentiate between scans

	if !f.schemaExists(db) {
		err = f.generateSchema(db)
		if err != nil {
			return fmt.Errorf("could not generate schema: %v", err)
		}
	}

	tx, err := db.Begin()
	if err != nil {
		panic(err)
	}

	scanID, err := f.insertScan(db, &td.NMAPRun)
	if err != nil {
		return fmt.Errorf("could not insert new scan: %v", err)
	}

	log.Printf("New scan with ID (%d) is inserted", scanID)

	for _, host := range td.NMAPRun.Host {
		hostID, err := f.insertReturnID(
			db,
			insertHostsSQL,
			scanID,
			"TODO",
			"TODO",
			host.StartTime,
			host.EndTime,
			host.Status.State,
			host.Status.Reason,
			host.Uptime.Seconds,
			host.Uptime.LastBoot,
			host.Distance.Value,
			host.TCPSequence.Index,
			host.TCPSequence.Difficulty,
			host.TCPSequence.Values,
			host.IPIDSequence.Class,
			host.IPIDSequence.Values,
			host.TCPTSSequence.Class,
			host.TCPTSSequence.Values,
			host.Trace.Port,
			host.Trace.Protocol,
			host.Status.State,
		)
		if err != nil {
			tx.Rollback()
			return err
		}

		log.Printf("New hostID (%d) is created", hostID)

		err = f.insertHostTracesHops(db, hostID, host.Trace.Hops)
		if err != nil {
			tx.Rollback()
			return err
		}

		log.Printf("Host trace hops are inserted for host ID (%d)", hostID)

		err = f.insertHostAddresses(db, hostID, host.HostAddress)
		if err != nil {
			tx.Rollback()
			return err
		}

		log.Printf("Host addresses are inserted for host ID (%d)", hostID)

		err = f.insertHostNames(db, hostID, &host.HostNames)
		if err != nil {
			tx.Rollback()
			return err
		}

		log.Printf("Host names are inserted for host ID (%d)", hostID)

		osID, err := f.insertOSRecords(db, hostID, &host.OS)
		if err != nil {
			tx.Rollback()
			return err
		}

		log.Printf("OS record is inserted ID (%d)", osID)

		err = f.insertOSPortUsed(db, osID, host.OS.OSPortUsed)
		if err != nil {
			tx.Rollback()
			return err
		}

		log.Printf("Insert OS port used for os ID (%d)", osID)

		err = f.insertOSMatch(db, osID, host.OS.OSMatch)
		if err != nil {
			tx.Rollback()
			return err
		}

		portInsert, err := db.Prepare(insertPortsSQL)
		if err != nil {
			tx.Rollback()
			return err
		}

		for _, portInsertRecord := range host.Port {
			result, err := portInsert.Exec(
				hostID,
				portInsertRecord.PortID,
				portInsertRecord.State.State,
				portInsertRecord.State.Reason,
				portInsertRecord.State.ReasonTTL,
				portInsertRecord.Service.Name,
				portInsertRecord.Service.Product,
				portInsertRecord.Service.Version,
				portInsertRecord.Service.ExtraInfo,
				portInsertRecord.Service.Method,
				portInsertRecord.Service.Conf,
				strings.Join(portInsertRecord.Service.CPE, sqliteStringDelimiter),
			)
			if err != nil {
				tx.Rollback()
				return err
			}

			portID, err := result.LastInsertId()
			if err != nil {
				tx.Rollback()
				return err
			}

			err = f.insertPortScripts(db, portID, portInsertRecord.Script)
			if err != nil {
				tx.Rollback()
				return err
			}
		}

		portInsert.Close()
	}

	if err != nil {
		tx.Commit()
	}

	return err
}

// defaultTemplateContent does not return anything
func (f *SqliteFormatter) defaultTemplateContent() string {
	return ""
}