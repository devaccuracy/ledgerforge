/*
Copyright 2024 Blnk Finance Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package model

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestPrecisionLookupAuthoritative(t *testing.T) {
	for _, tc := range []struct {
		name      string
		precision float64
		lookupErr error
		want      string
	}{
		{name: "authoritative scale", precision: 100, want: "101"},
		{name: "fractional scale", precision: 1.5},
		{name: "zero scale", precision: 0},
		{name: "lookup failed", lookupErr: errors.New("unavailable")},
		{name: "missing transaction", lookupErr: sql.ErrNoRows},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			query := mock.ExpectQuery("SELECT precision").WithArgs("txn_lookup")
			if tc.lookupErr != nil {
				query.WillReturnError(tc.lookupErr)
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"precision"}).AddRow(tc.precision))
			}
			txn := &Transaction{TransactionID: "txn_lookup", Amount: 1.005, Precision: 1000}
			got, err := ApplyPrecisionWithDBLookupChecked(txn, db)
			if tc.want == "" {
				require.Error(t, err)
				require.Nil(t, got)
				require.Equal(t, float64(1000), txn.Precision)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, got.String())
				require.Equal(t, tc.precision, txn.Precision)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
