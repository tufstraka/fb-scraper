// internal/utils/logger.go
package utils

import (
    "os"

    "github.com/sirupsen/logrus"
)

func SetupLogger(debug bool) {
    logrus.SetFormatter(&logrus.TextFormatter{
        FullTimestamp: true,
        TimestampFormat: "2006-01-02 15:04:05",
    })

    if debug {
        logrus.SetLevel(logrus.DebugLevel)
    } else {
        logrus.SetLevel(logrus.InfoLevel)
    }

    logrus.SetOutput(os.Stdout)
}