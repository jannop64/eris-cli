package util

import (
	"strings"

	. "github.com/eris-ltd/eris-cli/errors"

	. "github.com/eris-ltd/common/go/common"
	"github.com/spf13/viper"
)

func Edit(conf *viper.Viper, configVals []string) error {
	filePath := conf.ConfigFileUsed()
	if len(configVals) == 0 {
		if err := Editor(filePath); err != nil {
			return err
		}
	} else {
		for _, v := range configVals {
			spl := strings.Split(v, "=")
			if len(spl) != 2 {
				return &ErisError{404, BaseErrorES(ErrBadConfigOptions, v), ""}
			}
			key, val := spl[0], spl[1]
			spl = strings.Split(val, ",")
			if len(spl) > 1 {
				conf.Set(key, spl)
			} else {
				conf.Set(key, val)
			}
		}
	}

	return nil
}
