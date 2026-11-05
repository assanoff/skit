// Package partition keeps the range partitions of Postgres tables by time: it
// creates the partitions of the current period and of the next ones ahead, and
// drops the partitions older than the retention.
//
// A table partitioned by range of a time column has no place for a row of a
// period without a partition: with no default partition the insert fails. So
// the partitions are made ahead, and an old period goes as a whole — a DROP of
// its partition, without the DELETE, the bloat and the VACUUM.
//
// # Usage
//
// The migration creates the partitioned table alone; its primary key holds
// the partition key:
//
//	CREATE TABLE messages (
//	    id         uuid NOT NULL,
//	    created_at timestamptz NOT NULL,
//	    PRIMARY KEY (id, created_at)
//	) PARTITION BY RANGE (created_at);
//
// The partitions come from Maintain, run by a worker.Loop on every replica:
// the first tick at the start, then every hour. Maintain is a
// worker.TickFunc as it is:
//
//	keeper, err := partition.New(log, db, partition.Config{
//	    Tables:    []string{"messages", "deliveries"},
//	    Period:    partition.Month,
//	    Retention: 90 * 24 * time.Hour,
//	})
//	if err != nil {
//	    return err
//	}
//	group.Add(worker.NewLoop(log, worker.LoopConfig{
//	    Name:               "partitions",
//	    Interval:           time.Hour,
//	    ImmediateFirstTick: true,
//	    Metrics:            d.Metrics,
//	}, keeper.Maintain))
//
// A test calls Maintain itself, with the clock of WithNow, before it writes
// rows; a run at a later time shows the next periods and the drops.
//
// # Partitions
//
// A partition is named after its table and period, in the schema of the
// table: messages_2026_11 for Month, messages_2026_11_05 for Day. Its bounds
// are the starts of the periods in UTC, and a column of timestamptz, timestamp
// or date takes them. A partition of another name is neither counted nor
// dropped; one whose range overlaps a new partition fails the run, as does a
// default partition with rows of the new period.
//
// Maintain creates the partitions of the current period and of Ahead periods
// after it that are missing, table by table in the order of Config.Tables. It
// drops a partition once its end is Retention old, so no row younger than
// Retention goes; the tables go in the reverse order, a partition is detached
// and then dropped. A table that references another by a foreign key is
// listed after it: its rows go first.
//
// # Replicas and locks
//
// A run is one transaction. It takes a transaction-level advisory lock, one
// for every Keeper of the database, so the runs of the replicas take their
// turns: the second one finds nothing to do. Then each DDL statement waits
// for the lock of its table no longer than 10s (lock_timeout): a statement
// waiting behind a long query would hold every insert into the table behind
// itself. On the timeout the run fails, and the next tick does the work.
//
// # Alerts
//
// The package has no metrics of its own: the Loop records its ticks. Alert
// when the last success of the Loop is older than a part of the time Ahead
// leaves before inserts fail — a day for Month:
//
//	time() - max without(pod, instance) (
//	    skit_worker_last_success_timestamp_seconds{worker="partitions"}
//	) > 86400
//
// # Options
//
//   - Config: Tables, Period (Day or Month), Ahead (default 1), Retention
//     (0 keeps every partition).
//   - WithNow: the clock of the Keeper (tests); time.Now by default.
package partition
