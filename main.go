package main

import (
	"context"
	"fmt"
	"scoreinflux/db"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/mgo.v2/bson"
)

func main() {
	//	ctx, cancel := context.WithTimeout(context.Background(), ctxDeadline)
	//	defer cancel()
	client, err := mongo.Connect(context.TODO(), options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		fmt.Println("cannot connect to mongodb", err)
		return
	}

	cursor, err := client.Database("xstats").Collection("combinations").Find(context.TODO(), bson.M{"size": 1})
	if err != nil {
		fmt.Println("cannot get players", err)
		return
	}
	defer cursor.Close(context.TODO())

	var current db.CombinValue
	var complement db.CombinValue
	var all []*db.CombinValue
	for cursor.Next(context.TODO()) {
		if cursor.Decode(&current) != nil {
			fmt.Println("cursor error", err)
			return
		}

		cursor2, err := client.Database("xstats").Collection("combinations").Find(context.TODO(),
			bson.M{"size": 2, "players": current.Players[0], "minutes": bson.M{"$gt": 90}})
		if err != nil {
			fmt.Println("cannot get pairs for "+current.Players[0], err)
			return
		}

		if cursor2.All(context.TODO(), &all) != nil {
			fmt.Println("cursor2 error", err)
			return
		}

		shap := current.Score * float64(len(all))

		players := make([]string, 0, len(all))
		for _, x := range all {
			if x.Players[0] == current.Players[0] {
				players = append(players, x.Players[1])
			} else {
				players = append(players, x.Players[0])
			}

			shap += x.Score
		}

		cursor2, err = client.Database("xstats").Collection("combinations").Find(context.TODO(),
			bson.M{"size": 1, "players": bson.M{"$in": players}})
		if err != nil {
			fmt.Println("cannot get complements for "+current.Players[0], err)
			return
		}

		for cursor2.Next(context.TODO()) {
			if cursor.Decode(&complement) != nil {
				fmt.Println("cursor error", err)
				cursor2.Close(context.TODO())
				return
			}

			shap -= complement.Score
		}

		cursor2.Close(context.TODO())

		shap /= 2 * float64(len(all))

		_, err = client.Database("xstats").Collection("ratings").UpdateByID(
			context.TODO(),
			current.Players[0],
			bson.M{"$set": bson.M{"rating": shap}},
			options.Update().SetUpsert(true),
		)
		if err != nil {
			fmt.Println("update error", err)
			return
		}
	}
}
