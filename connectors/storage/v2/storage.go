// SPDX-License-Identifier: ice License 1.0

package storage

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	stdlibtime "time"

	"github.com/cenkalti/backoff/v4"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/hashicorp/go-multierror"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pkg/errors"

	appcfg "github.com/ice-blockchain/wintr/config"
	"github.com/ice-blockchain/wintr/log"
)

//nolint:gochecknoinits // GlobalDB is single instance, we initialize it here.
func init() {
	var cfg storageCfg
	appcfg.MustLoadFromKey(globalDBYamlKey, &cfg)
	if cfg.PrimaryURL != "" {
		initGlobalDBCtx, cancel := context.WithTimeout(context.Background(), 30*stdlibtime.Second) //nolint:gomnd,mnd // .
		defer cancel()
		globalDB = mustConnectWithCfg(initGlobalDBCtx, &cfg, "")
	}
}

func GlobalDB() *DB {
	if globalDB == nil {
		log.Panic(errors.Errorf("global db is not initialized, check %v", globalDBYamlKey))
	}

	return globalDB
}

func MustConnect(ctx context.Context, ddl, applicationYAMLKey string) *DB {
	var cfg config
	appcfg.MustLoadFromKey(applicationYAMLKey, &cfg)

	return mustConnectWithCfg(ctx, &cfg.WintrStorage, ddl)
}

func mustConnectWithCfg(ctx context.Context, cfg *storageCfg, ddl string) *DB {
	var replicas []*pgxpool.Pool
	var master *pgxpool.Pool
	if cfg.PrimaryURL != "" {
		master = mustConnectPool(ctx, cfg.Timeout, cfg.Credentials.User, cfg.Credentials.Password, cfg.PrimaryURL)
	}
	for ix, url := range cfg.ReplicaURLs {
		if ix == 0 {
			replicas = make([]*pgxpool.Pool, len(cfg.ReplicaURLs)) //nolint:makezero // Not needed, we know the size.
		}
		replicas[ix] = mustConnectPool(ctx, cfg.Timeout, cfg.Credentials.User, cfg.Credentials.Password, url)
	}
	if master != nil && ddl != "" && cfg.RunDDL {
		for _, statement := range strings.Split(ddl, "----") {
			_, err := master.Exec(ctx, statement)
			log.Panic(errors.Wrapf(err, "failed to run statement: %v", statement))
		}
	}
	db := &DB{master: master, lb: &lb{replicas: replicas}, acquiredLocks: make(map[int64]*pgxpool.Conn)}

	return db
}

//nolint:mnd,gomnd // Configuration.
func mustConnectPool(ctx context.Context, timeout, user, pass, url string) (db *pgxpool.Pool) {
	poolConfig, err := pgxpool.ParseConfig(url)
	log.Panic(errors.Wrapf(err, "failed to parse pool config: %v", url)) //nolint:revive // Intended
	poolConfig.ConnConfig.User = user
	poolConfig.ConnConfig.Password = pass
	poolConfig.ConnConfig.StatementCacheCapacity = 1024
	poolConfig.ConnConfig.DescriptionCacheCapacity = 1024
	poolConfig.ConnConfig.Config.ConnectTimeout = 30 * stdlibtime.Second
	if !strings.Contains(strings.ToLower(url), "pool_max_conn_idle_time") {
		poolConfig.MaxConnIdleTime = stdlibtime.Minute
	}
	log.Info(fmt.Sprintf("poolConfig.MaxConnIdleTime=%v", poolConfig.MaxConnIdleTime))
	if !strings.Contains(strings.ToLower(url), "pool_health_check_period") {
		poolConfig.HealthCheckPeriod = 30 * stdlibtime.Second
	}
	log.Info(fmt.Sprintf("poolConfig.HealthCheckPeriod=%v", poolConfig.HealthCheckPeriod))
	poolConfig.MaxConnLifetimeJitter = 10 * stdlibtime.Minute
	poolConfig.MaxConnLifetime = 24 * stdlibtime.Hour
	poolConfig.AfterConnect = func(cctx context.Context, conn *pgx.Conn) error { return doAfterConnect(cctx, timeout, conn) }
	poolConfig.MinConns = 1
	db, err = pgxpool.NewWithConfig(ctx, poolConfig)
	log.Panic(errors.Wrapf(err, "failed to start pool for config: %v", url))

	return db
}

func doAfterConnect(ctx context.Context, timeout string, conn *pgx.Conn) error { //nolint:funlen // .
	actualTimeout := "30s"
	if timeout != "" {
		actualTimeout = timeout
	}
	log.Info(fmt.Sprintf("wintr/connectors/storage/v2:timeout:%v", timeout))
	customConnectionParameters := map[string]string{
		"statement_timeout":                   actualTimeout,
		"idle_in_transaction_session_timeout": actualTimeout,
		"lock_timeout":                        actualTimeout,
		// "tcp_user_timeout":                 actualTimeout,.
		"enable_partitionwise_join":      "on",
		"enable_partitionwise_aggregate": "on",
	}
	values := make([]string, 0, len(customConnectionParameters))
	for name, setting := range customConnectionParameters {
		values = append(values, fmt.Sprintf("'%v'", name))
		if _, qErr := conn.Exec(ctx, fmt.Sprintf(`SET %v = '%v'`, name, setting)); qErr != nil {
			return qErr //nolint:wrapcheck // Not needed.
		}
	}
	sql := fmt.Sprintf(`SELECT name, setting
							FROM pg_settings
							WHERE name IN (%v)`, strings.Join(values, ","))
	rows, qErr := conn.Query(ctx, sql)
	if qErr != nil {
		return errors.Wrapf(qErr, "validation select failed")
	}
	var res []*struct{ Name, Setting string }
	if qErr = pgxscan.ScanAll(&res, rows); qErr != nil {
		return errors.New("scanning validation select rows failed")
	}
	actual := make(map[string]string, len(res))
	for _, row := range res {
		actual[row.Name] = strings.ReplaceAll(row.Setting, "0000", "0s")
	}
	if !reflect.DeepEqual(actual, customConnectionParameters) {
		return errors.Errorf("db validation failed, expected:%#v, actual:%#v", customConnectionParameters, actual)
	}

	return nil
}

func (db *DB) registerLock(conn *pgxpool.Conn, lock *advisoryLockMutex) {
	db.locksMx.Lock()
	defer db.locksMx.Unlock()
	db.acquiredLocks[lock.id] = conn
}

func (db *DB) Close() error {
	db.locksMx.Lock()
	for lockID, conn := range db.acquiredLocks {
		conn.Release()
		delete(db.acquiredLocks, lockID)
	}
	db.locksMx.Unlock()
	if db.master != nil {
		db.master.Close()
	}
	if len(db.lb.replicas) != 0 {
		for _, replica := range db.lb.replicas {
			replica.Close()
		}
	}
	db.closedMx.Lock()
	defer db.closedMx.Unlock()
	db.closed = true

	return nil
}

func (db *DB) Ping(ctx context.Context) error {
	wg := new(sync.WaitGroup)
	errChan := make(chan error, len(db.lb.replicas)+1)
	if db.master != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errChan <- errors.Wrap(db.master.Ping(ctx), "ping failed for master")
		}()
	}
	if len(db.lb.replicas) != 0 {
		wg.Add(len(db.lb.replicas))
		for ii := range db.lb.replicas {
			go func(ix int) {
				defer wg.Done()
				errChan <- errors.Wrapf(db.lb.replicas[ix].Ping(ctx), "ping failed for replica[%v]", ix)
			}(ii)
		}
	}
	wg.Wait()
	close(errChan)
	errs := make([]error, 0, len(db.lb.replicas)+1)
	for err := range errChan {
		errs = append(errs, err)
	}

	return multierror.Append(nil, errs...).ErrorOrNil() //nolint:wrapcheck // Not needed.
}

func (db *DB) primary() *pgxpool.Pool {
	return db.master
}

func (db *DB) replica() *pgxpool.Pool {
	return db.lb.replicas[atomic.AddUint64(&db.lb.currentIndex, 1)%uint64(len(db.lb.replicas))]
}

func (*DB) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	panic("should not be used because its implemented just for type matching")
}

func (*DB) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	panic("should not be used because its implemented just for type matching")
}

func retry[T any](ctx context.Context, op func() (T, error)) (tt T, err error) {
	err = backoff.RetryNotify(
		func() error {
			tt, err = op()

			return err
		},
		//nolint:mnd,gomnd // Because those are static configs.
		backoff.WithContext(&backoff.ExponentialBackOff{
			InitialInterval:     100 * stdlibtime.Millisecond,
			RandomizationFactor: 0.5,
			Multiplier:          2.5,
			MaxInterval:         stdlibtime.Second,
			MaxElapsedTime:      25 * stdlibtime.Second,
			Stop:                backoff.Stop,
			Clock:               backoff.SystemClock,
		}, ctx),
		func(e error, next stdlibtime.Duration) {
			log.Error(errors.Wrapf(e, "[wintr/storage/v2]call failed. retrying in %v... ", next))
		})

	return tt, err
}
