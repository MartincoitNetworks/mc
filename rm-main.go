/*
 * Minio Client (C) 2014, 2015 Minio, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"path/filepath"
	"strings"

	"github.com/minio/cli"
	"github.com/minio/mc/pkg/client"
	"github.com/minio/minio-xl/pkg/probe"
)

// remove a file or folder.
var rmCmd = cli.Command{
	Name:   "rm",
	Usage:  "Remove file or bucket.",
	Action: mainRm,
	CustomHelpTemplate: `NAME:
   mc {{.Name}} - {{.Usage}}

USAGE:
   mc {{.Name}} TARGET [incomplete] [force]

   incomplete - remove incomplete uploads
   force      - force recursive remove

EXAMPLES:
   1. Remove a file on Cloud storage
     $ mc {{.Name}} https://s3.amazonaws.com/jazz-songs/louis/file01.mp3

   2. Remove a folder recursively on Cloud storage
     $ mc {{.Name}} https://s3.amazonaws.com/jazz-songs/louis/... force

   3. Remove a bucket on Minio cloud storage
     $ mc {{.Name}} https://play.minio.io:9000/mongodb-backup

   4. Remove a bucket on Cloud storage recursively
     $ mc {{.Name}} https://s3.amazonaws.com/jazz-songs/... force

   5. Remove a file on local filesystem:
      $ mc {{.Name}} march/expenses.doc

   6. Remove a file named "force" on local filesystem:
      $ mc {{.Name}} force

   7. Remove incomplete upload of a file on Cloud storage:
      $ mc {{.Name}} https://s3.amazonaws.com/jazz-songs/louis/file01.mp3 incomplete

   2. Remove incomplete uploads of folder recursively on Cloud storage
      $ mc {{.Name}} https://s3.amazonaws.com/jazz-songs/louis/... incomplete force

`,
}

func rmList(url string) (<-chan string, *probe.Error) {
	clnt, err := url2Client(url)
	if err != nil {
		errorIf(err.Trace(), "Unable to get client object for "+url)
		return nil, err.Trace()
	}
	in := clnt.List(true, false)
	out := make(chan string)

	var depthFirst func(currentDir string) (*client.Content, bool)

	depthFirst = func(currentDir string) (*client.Content, bool) {
		entry, ok := <-in
		for {
			if !ok || !strings.HasPrefix(entry.Content.Name, currentDir) {
				return entry.Content, ok
			}
			if entry.Content.Type.IsRegular() {
				out <- entry.Content.Name
			}
			if entry.Content.Type.IsDir() {
				var content *client.Content
				content, ok = depthFirst(entry.Content.Name)
				out <- entry.Content.Name
				entry = client.ContentOnChannel{Content: content}
				continue
			}
			entry, ok = <-in
		}
	}

	go func() {
		depthFirst("")
		close(out)
	}()
	return out, nil
}

func rmSingle(url string) {
	clnt, err := url2Client(url)
	if err != nil {
		errorIf(err.Trace(), "Unable to get client object for "+url+".")
		return
	}
	err = clnt.Remove()
	errorIf(err.Trace(), "Unable to remove "+url+".")
}

func rmAll(url string) {
	urlPartial1 := url2Dir(url)
	out, err := rmList(url)
	if err != nil {
		errorIf(err.Trace(), "Unable to List "+url+".")
		return
	}
	for urlPartial2 := range out {
		urlFull := filepath.Join(urlPartial1, urlPartial2)
		newclnt, e := url2Client(urlFull)
		if e != nil {
			errorIf(e, "Unable to create client object : "+urlFull+".")
			continue
		}
		err = newclnt.Remove()
		errorIf(err, "Unable to remove : "+urlFull+".")
	}
}

func rmIncompleteUpload(url string) {
	clnt, err := url2Client(url)
	if err != nil {
		errorIf(err.Trace(), "Unable to get client object for "+url+".")
		return
	}
	err = clnt.RemoveIncompleteUpload()
	errorIf(err.Trace(), "Unable to remove "+url+".")
}

func rmAllIncompleteUploads(url string) {
	clnt, err := url2Client(url)
	if err != nil {
		errorIf(err.Trace(), "Unable to get client object for "+url+".")
		return
	}
	urlPartial1 := url2Dir(url)
	ch := clnt.List(true, true)
	for entry := range ch {
		urlFull := filepath.Join(urlPartial1, entry.Content.Name)
		newclnt, e := url2Client(urlFull)
		if e != nil {
			errorIf(e, "Unable to create client object : "+urlFull+".")
			continue
		}
		err = newclnt.RemoveIncompleteUpload()
		errorIf(err, "Unable to remove : "+urlFull+".")
	}
}

func checkRmSyntax(ctx *cli.Context) {
	args := ctx.Args()

	var force bool
	var incomplete bool
	if !args.Present() || args.First() == "help" {
		cli.ShowCommandHelpAndExit(ctx, "rm", 1) // last argument is exit code.
	}
	if len(args) == 1 && args.Get(0) == "force" {
		return
	}
	if len(args) == 2 && args.Get(0) == "force" && args.Get(1) == "incomplete" ||
		len(args) == 2 && args.Get(1) == "force" && args.Get(0) == "incomplete" {
		return
	}
	if args.Last() == "force" {
		force = true
		args = args[:len(args)-1]
	}
	if args.Last() == "incomplete" {
		incomplete = true
		args = args[:len(args)-1]
	}

	// By this time we have sanitized the input args and now we have only the URLs parse them properly
	// and validate.
	URLs, err := args2URLs(args)
	fatalIf(err.Trace(ctx.Args()...), "Unable to parse arguments.")

	// If input validation fails then provide context sensitive help without displaying generic help message.
	// The context sensitive help is shown per argument instead of all arguments to keep the help display
	// as well as the code simple. Also most of the times there will be just one arg
	for _, url := range URLs {
		u := client.NewURL(url)
		var helpStr string
		if strings.HasSuffix(url, string(u.Separator)) {
			if incomplete {
				helpStr = "Usage : mc rm " + url + recursiveSeparator + " incomplete force"
			} else {
				helpStr = "Usage : mc rm " + url + recursiveSeparator + " force"
			}
			fatalIf(errDummy().Trace(), helpStr)
		}
		if isURLRecursive(url) && !force {
			if incomplete {
				helpStr = "Usage : mc rm " + url + " incomplete force"
			} else {
				helpStr = "Usage : mc rm " + url + " force"
			}
			fatalIf(errDummy().Trace(), helpStr)
		}
	}
}

func mainRm(ctx *cli.Context) {
	checkRmSyntax(ctx)
	var incomplete bool
	var force bool

	args := ctx.Args()
	if len(args) != 1 {
		if len(args) == 2 && args.Get(0) == "force" && args.Get(1) == "incomplete" ||
			len(args) == 2 && args.Get(0) == "incomplete" && args.Get(1) == "force" {
			args = args[:]
		} else {
			if args.Last() == "force" {
				force = true
				args = args[:len(args)-1]
			}
			if args.Last() == "incomplete" {
				incomplete = true
				args = args[:len(args)-1]
			}
		}
	}

	URLs, err := args2URLs(args)
	fatalIf(err.Trace(ctx.Args()...), "Unable to parse arguments.")

	// execute for incomplete
	if incomplete {
		for _, url := range URLs {
			if isURLRecursive(url) && force {
				rmAllIncompleteUploads(stripRecursiveURL(url))
			} else {
				rmIncompleteUpload(url)
			}
		}
		return
	}
	for _, url := range URLs {
		if isURLRecursive(url) && force {
			rmAll(stripRecursiveURL(url))
		} else {
			rmSingle(url)
		}
	}
}
