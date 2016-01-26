//  Copyright (c) 2015-2016 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	go_n1ql "github.com/couchbase/go_n1ql"
	"github.com/couchbase/query/errors"
	"github.com/couchbase/query/shell/go_cbq/command"
	"github.com/sbinet/liner"
)

/* The following values define the query prompt for cbq.
   The expected end of line character is a ;.
*/
const (
	QRY_EOL     = ";"
	QRY_PROMPT1 = "> "
	QRY_PROMPT2 = "   > "
)

var first = false

var homeDir string

/* This method is used to handle user interaction with the
   cli. After combining the multi line input, it is sent to
   the execute_inpu method which parses and executes the
   input command. In the event an error is returned from the
   query execution, it is printed in red. The input prompt is
   the name of the executable.
*/
func HandleInteractiveMode(prompt string) {

	// Variables used for output to file
	var err error
	outputFile := os.Stdout
	prevFile := ""
	prevreset := command.Getreset()
	prevfgRed := command.GetfgRed()

	// If an output flag is defined
	if outputFlag != "" {
		prevFile = command.FILE_OUTPUT
		outputFile, err = os.OpenFile(command.FILE_OUTPUT, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
		command.SetDispVal("", "")
		if err != nil {
			s_err := command.HandleError(errors.FILE_OPEN, err.Error())
			command.PrintError(s_err)
		}

		defer outputFile.Close()
		command.SetWriter(io.Writer(outputFile))
	}

	/* Find the HOME environment variable. If it isnt set then
	   try USERPROFILE for windows. If neither is found then
	   the cli cant find the history file to read from.
	*/
	homeDir = os.Getenv("HOME")
	if homeDir == "" {
		homeDir = os.Getenv("USERPROFILE")
		if homeDir == "" {
			_, werr := io.WriteString(command.W, "Unable to determine home directory, history file disabled\n")
			if werr != nil {
				s_err := command.HandleError(errors.WRITER_OUTPUT, werr.Error())
				command.PrintError(s_err)
			}
		}
	}

	/* Create a new liner */
	var liner = liner.NewLiner()
	defer liner.Close()

	/* Load history from Home directory
	 */
	err_code, err_string := LoadHistory(liner, homeDir)
	if err_code != 0 {
		s_err := command.HandleError(err_code, err_string)
		command.PrintError(s_err)
	}

	go signalCatcher(liner)

	// state for reading a multi-line query
	inputLine := []string{}
	fullPrompt := prompt + QRY_PROMPT1

	// Handle the file input and script options here so as to add
	// the commands to the history.
	if scriptFlag != "" {
		//Execute the input command
		go_n1ql.SetPassthroughMode(true)

		// If outputting to a file, then add the statement to the file as well.
		if command.FILE_WR_MODE == true {
			_, werr := io.WriteString(command.W, scriptFlag+"\n")
			if werr != nil {
				s_err := command.HandleError(errors.WRITER_OUTPUT, werr.Error())
				command.PrintError(s_err)
			}
		}

		err_code, err_str := execute_input(scriptFlag, command.W, false, liner)
		if err_code != 0 {
			s_err := command.HandleError(err_code, err_str)
			command.PrintError(s_err)
			liner.Close()
			os.Clearenv()
			os.Exit(1)
		}
		liner.Close()
		os.Clearenv()
		os.Exit(0)
	}

	if inputFlag != "" {
		//Read each line from the file and call execute query
		go_n1ql.SetPassthroughMode(true)
		input_command := "\\source " + inputFlag

		// If outputting to a file, then add the statement to the file as well.
		if command.FILE_WR_MODE == true {
			_, werr := io.WriteString(command.W, input_command+"\n")
			if werr != nil {
				s_err := command.HandleError(errors.WRITER_OUTPUT, werr.Error())
				command.PrintError(s_err)
			}
		}

		errCode, errStr := execute_input(input_command, command.W, false, liner)
		if errCode != 0 {
			s_err := command.HandleError(errCode, errStr)
			command.PrintError(s_err)
			liner.Close()
			os.Clearenv()
			os.Exit(1)
		}
		liner.Close()
		os.Clearenv()
		os.Exit(0)
	}
	// End handling the options

	for {
		line, err := liner.Prompt(fullPrompt)
		if err != nil {
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Redirect command
		prevFile, outputFile = redirectTo(prevFile, prevreset, prevfgRed)

		if outputFile == os.Stdout {
			command.SetDispVal(prevreset, prevfgRed)
			command.SetWriter(os.Stdout)
		} else {
			if outputFile != nil {
				defer outputFile.Close()
				command.SetWriter(io.Writer(outputFile))
			}
		}
		/* Check for shell comments : -- and #. Add them to the history
		   but do not send them to be parsed.
		*/
		if strings.HasPrefix(line, "--") || strings.HasPrefix(line, "#") {
			err_code, err_string := UpdateHistory(liner, homeDir, line)
			if err_code != 0 {
				s_err := command.HandleError(err_code, err_string)
				command.PrintError(s_err)
			}

			continue
		}

		// Building query string mode: set prompt, gather current line
		fullPrompt = QRY_PROMPT2
		inputLine = append(inputLine, line)

		/* If the current line ends with a QRY_EOL, join all query lines,
		   trim off trailing QRY_EOL characters, and submit the query string.
		*/
		if strings.HasSuffix(line, QRY_EOL) {
			inputString := strings.Join(inputLine, " ")
			for strings.HasSuffix(inputString, QRY_EOL) {
				inputString = strings.TrimSuffix(inputString, QRY_EOL)
			}
			if inputString != "" {
				err_code, err_string := UpdateHistory(liner, homeDir, inputString+QRY_EOL)
				if err_code != 0 {
					s_err := command.HandleError(err_code, err_string)
					command.PrintError(s_err)
				}
				// If outputting to a file, then add the statement to the file as well.
				if command.FILE_WR_MODE == true {
					_, werr := io.WriteString(command.W, "\n"+inputString+"\n")
					if werr != nil {
						s_err := command.HandleError(errors.WRITER_OUTPUT, werr.Error())
						command.PrintError(s_err)
					}
				}
				err_code, err_string = execute_input(inputString, command.W, true, liner)
				/* Error handling for Shell errors and errors recieved from
				   go_n1ql.
				*/
				if err_code != 0 {
					s_err := command.HandleError(err_code, err_string)
					if err_code == errors.GON1QL_QUERY {
						//Dont print the error code for query errors.
						tmpstr := fmt.Sprintln(command.GetfgRed(), s_err, command.Getreset())
						io.WriteString(command.W, tmpstr+"\n")

					} else {
						command.PrintError(s_err)
					}

					if *errorExitFlag == true {
						if first == false {
							first = true
							_, werr := io.WriteString(command.W, "Exiting on first error encountered\n")
							if werr != nil {
								s_err = command.HandleError(errors.WRITER_OUTPUT, werr.Error())
								command.PrintError(s_err)
							}
							liner.Close()
							os.Clearenv()
							os.Exit(1)
						}
					}
				}

				/* For the \EXIT and \QUIT shell commands we need to
				   make sure that we close the liner and then exit. In
				   the event an error is returned from execute_input after
				   the \EXIT command, then handle the error and exit with
				   exit code 1 (which is for general errors).
				*/
				if EXIT == true {
					command.EXIT = false
					liner.Close()
					if err == nil {
						os.Exit(0)
					} else {
						os.Exit(1)
					}

				}

			}

			// reset state for multi-line query
			inputLine = []string{}
			fullPrompt = prompt + QRY_PROMPT1
		}
	}

}

/* If ^C is pressed then Abort the shell. This is
   provided by the liner package.
*/
func signalCatcher(liner *liner.State) {
	liner.SetCtrlCAborts(false)

}

func redirectTo(prevFile, prevreset, prevfgRed string) (string, *os.File) {
	var err error
	var outputFile *os.File

	if command.FILE_WR_MODE == true {
		if prevFile != command.FILE_OUTPUT {
			prevFile = command.FILE_OUTPUT
			outputFile, err = os.OpenFile(command.FILE_OUTPUT, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
			command.SetDispVal("", "")
			if err != nil {
				s_err := command.HandleError(errors.FILE_OPEN, err.Error())
				command.PrintError(s_err)
				return prevFile, nil
			}

		}
	} else {
		command.SetDispVal(prevreset, prevfgRed)
		prevFile = ""
		outputFile = os.Stdout
	}
	return prevFile, outputFile
}
