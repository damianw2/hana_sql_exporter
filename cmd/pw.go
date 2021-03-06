// Copyright © 2020 Ulrich Anhalt <ulrich.anhalt@gmail.com>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"
	"strings"

	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/ulranh/hana_sql_exporter/internal"
	"golang.org/x/crypto/ssh/terminal"
)

// pwCmd represents the pw command
var pwCmd = &cobra.Command{
	Use:   "pw",
	Short: "Set passwords for the tenants in the config file",
	Long: `With the command pw you can set the passwords for the tenants you want to monitor. You can set the password for one tenant or several tenants separated by comma. For example:
	hana_sql_exporter pw --tenant d01
	hana_sql_exporter pw -t d01,d02 --config ./.hana_sql_exporter.toml`,
	Run: func(cmd *cobra.Command, args []string) {

		config, err := getConfig()
		if err != nil {
			exit("Can't handle config file: ", err)
		}

		err = config.setPw(cmd)
		if err != nil {
			exit("Can't set password: ", err)
		}
	},
}

func init() {
	RootCmd.AddCommand(pwCmd)

	pwCmd.PersistentFlags().StringP("tenant", "t", "", "name(s) of tenant(s) separated by comma")
	pwCmd.MarkPersistentFlagRequired("tenant")
}

// save password(s) of tenant(s) database user to the config file
func (config *Config) setPw(cmd *cobra.Command) error {

	fmt.Print("Password: ")
	pw, err := terminal.ReadPassword(0)
	if err != nil {
		return errors.Wrap(err, "setPw(ReadPassword)")
	}

	tenants, err := cmd.Flags().GetString("tenant")
	if err != nil {
		return errors.Wrap(err, "setPw(GetString)")
	}

	err = config.newSecret(tenants, pw)
	if err != nil {
		return errors.Wrap(err, "setPw(newSecret)")
	}

	return nil
}

// create encrypted secret map for tenant(s)
func (config *Config) newSecret(tenants string, pw []byte) error {
	var err error

	// fill map with existing secrets from configfile
	var secret internal.Secret
	if err = proto.Unmarshal(config.Secret, &secret); err != nil {
		return errors.Wrap(err, "newSecret(Unmarshal)")
	}

	// create secret key once if it doesn't exist
	if _, ok := secret.Name["secretkey"]; !ok {

		secret.Name = make(map[string][]byte)
		secret.Name["secretkey"], err = internal.GetSecretKey()
		if err != nil {
			return errors.Wrap(err, "newSecret(getSecretKey)")
		}
	}

	// encrypt password
	encPw, err := internal.PwEncrypt(pw, secret.Name["secretkey"])
	if err != nil {
		return errors.Wrap(err, "newSecret(PwEncrypt)")
	}

	// determine the tenants specified in the command line
	tMap := make(map[string]bool)
	for _, tenant := range strings.Split(tenants, ",") {
		tMap[strings.ToLower(tenant)] = false
	}

	for _, tenant := range config.Tenants {
		tName := strings.ToLower(tenant.Name)

		// check if cmd line tenant exists in configfile tenants slice
		if _, ok := tMap[tName]; !ok {
			continue
		}
		tMap[tName] = true

		// connection test
		db := dbConnect(tenant.ConnStr, tenant.User, string(pw))
		defer db.Close()

		if err := dbPing(tName, db); err != nil {
			continue
		}

		// add password to secret map
		secret.Name[tName] = encPw
	}

	for k, v := range tMap {
		if !v {
			log.WithFields(log.Fields{
				"tenant": k,
			}).Error("Did not find tenant in configfile tenants slice.")
		}
	}

	// write pw information back to the config file
	config.Secret, err = proto.Marshal(&secret)
	if err != nil {
		return errors.Wrap(err, "newSecret(Marshal)")
	}
	viper.Set("secret", config.Secret)

	err = viper.WriteConfig()
	if err != nil {
		return errors.Wrap(err, "newSecret(WriteConfig)")
	}

	return nil
}

// decrypt password
func getPw(secret internal.Secret, name string) (string, error) {

	// get encrypted tenant pw
	if _, ok := secret.Name[name]; !ok {
		return "", errors.New("encrypted tenant pw info does not exist")
	}

	// decrypt tenant password
	pw, err := internal.PwDecrypt(secret.Name[name], secret.Name["secretkey"])
	if err != nil {
		return "", errors.Wrap(err, "getPW(PwDecrypt)")
	}
	return pw, nil
}
