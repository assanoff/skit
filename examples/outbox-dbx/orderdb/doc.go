// Package orderdb is the Postgres store of the orders on dbx. A Store over
// the pool opens a transaction for WithinTran and WithinTranTasks; a Store
// over a transaction (*dbx.Tx) joins it and does not commit. WithTasks gives
// the callback of WithinTranTasks the scheduler of the transaction.
//
// # Usage
//
//	store := orderdb.NewStore(log, db, orderdb.WithTasks(tasks))
//	if err := store.EnsureSchema(ctx); err != nil {
//		return err
//	}
//	core := orderapp.New(store)
//
// tasks is the func(tx *dbx.Tx) *queue.Scheduler of the assembly. Without
// WithTasks, Schedule fails rather than lose the task.
package orderdb
