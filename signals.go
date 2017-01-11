/*-
 * Copyright 2015 Square Inc.
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
	"io"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

// signalHandler. Listens for incoming SIGTERM or SIGUSR1 signals. If we get
// SIGTERM, stop listening for new connections and gracefully terminate the
// process. If we get SIGUSR1, reload certificates.
func (context *Context) signalHandler(proxy *proxy, closeables []io.Closer) {
	signals := make(chan os.Signal)
	signal.Notify(signals, syscall.SIGUSR1, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)

	for {
		// Wait for a signal
		select {
		case sig := <-signals:
			switch sig {
			case syscall.SIGINT, syscall.SIGTERM:
				logger.Printf("received %s, shutting down", sig.String())

				// Best-effort shutdown of status listener
				if context.statusHTTP != nil {
					go context.statusHTTP.Shutdown(nil)
				}

				// Force-exit after timeout (but make sure we terminate child)
				time.AfterFunc(*shutdownTimeout, func() {
					// Graceful shutdown timeout reached. If we can't drain connections
					// to exit gracefully after this timeout, let's just kill our child
					// process and exit so we don't hang forever.
					logger.Printf("graceful shutdown timeout: forcing exit")
					context.terminateChild(5 * time.Second)
					exitFunc(1)
				})

				atomic.StoreInt32(&proxy.quit, 1)
				for _, closeable := range closeables {
					closeable.Close()
				}

				logger.Printf("done with signal handler")
				return

			case syscall.SIGUSR1:
				logger.Printf("received %s, reloading certificates", sig.String())
				context.status.Reloading()
				err := context.cert.reload()
				if err != nil {
					logger.Printf("error reloading: %s", err)
				}
				logger.Printf("reloading complete")
				context.status.Listening()
			}
		case _ = <-context.watcher:
			context.status.Reloading()
			err := context.cert.reload()
			if err != nil {
				logger.Printf("error reloading: %s", err)
			}
			logger.Printf("reloading complete")
			context.status.Listening()
		}
	}
}
