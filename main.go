package main

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/google/uuid"
	"github.com/xareyx/ct-error/emulate"
	"google.golang.org/api/iterator"
)

func main() {
	ctx := context.Background()
	cfg := emulate.DefaultConfig()
	cfg.Database = "testdb"
	cfg.DDL = emulate.ParseDDL(spannerDDL)
	spannerEmulator := emulate.New(cfg, emulate.DefaultEmulator)
	err := spannerEmulator.Run(ctx)
	if err != nil {
		fmt.Printf("Can't start spanner emulator: %s", err)
		return
	}
	defer spannerEmulator.Close(ctx)

	dbname := "projects/" + cfg.Project + "/instances/" + cfg.Instance + "/databases/" + cfg.Database
	c, _ := spanner.NewClient(ctx, dbname)
	id := uuid.New().String()
	ti := time.Date(1970, 1, 1, 1, 1, 1, 1, time.UTC)

	_, dbErr1 := c.ReadWriteTransaction(ctx, func(ctx context.Context, readWriteTxn *spanner.ReadWriteTransaction) error {
		//insert
		testItem := &Test{
			ID: id,
			T1: spanner.NullTime{Valid: true, Time: ti},
			T2: spanner.NullTime{Valid: true, Time: ti},
		}
		mut, mutErr := spanner.InsertStruct("test", testItem)
		if mutErr != nil {
			return mutErr
		}
		err = readWriteTxn.BufferWrite([]*spanner.Mutation{mut})
		if err != nil {
			fmt.Printf("Failed to insert struct: %s", err)
			return err
		}

		return nil
	})

	if dbErr1 != nil {
		fmt.Printf("failed to insert: %s", dbErr1)
		return
	}

	_, dbErr2 := c.ReadWriteTransaction(ctx, func(ctx context.Context, readWriteTxn *spanner.ReadWriteTransaction) error {
		//get to confirm
		getStmt := spanner.Statement{
			SQL: "SELECT * FROM test WHERE id = @ID",
			Params: map[string]interface{}{
				"ID": id,
			},
		}

		var iter *spanner.RowIterator
		iter = readWriteTxn.Query(ctx, getStmt)
		defer iter.Stop()
		row, err := iter.Next()
		if err == iterator.Done {
			return fmt.Errorf("Machine not found")
		}
		if err != nil {
			return fmt.Errorf("error occured while getting Machine '%s': %w", id, err)
		}

		m := &Test{}
		if err := row.ToStruct(m); err != nil {
			return fmt.Errorf("Could not parse row into machine struct: %w", err)
		}

		fmt.Printf("get machine:\n%+v\n", m)

		//update that should be working
		badArgs := map[string]interface{}{
			"id": id,
			"T1": spanner.CommitTimestamp,
			"T2": spanner.CommitTimestamp,
		}
		badQuery := "UPDATE test SET t1=@T1, t2=@T2 WHERE id = @id"

		badStmt := spanner.Statement{
			SQL:    badQuery,
			Params: badArgs,
		}
		_, _ = readWriteTxn.Update(ctx, badStmt)
		// errors with "Invalid timestamp: 'spanner.commit_timestamp()'"

		//update that works
		goodArgs := map[string]interface{}{
			"id": id,
		}
		goodQuery := "UPDATE test SET t1=PENDING_COMMIT_TIMESTAMP(), t2=PENDING_COMMIT_TIMESTAMP() WHERE id = @id"
		goodStmt := spanner.Statement{
			SQL:    goodQuery,
			Params: goodArgs,
		}
		count, updateErr := readWriteTxn.Update(ctx, goodStmt)
		if updateErr != nil {
			return updateErr
		}
		if count == 0 {
			return fmt.Errorf("updated 0 rows")
		}

		return nil
	})

	if dbErr2 != nil {
		fmt.Println("!ok: ", dbErr2)
	} else {
		fmt.Println("ok")
	}
}
