package main

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/mongo/driver/uuid"
)

func (c *Context) InsertOrUpdateCombin(players []string, min int, score float64, home bool) error {
	if !home {
		score = -score
	}

	//	ctx, cancel := context.WithTimeout(context.Background(), ctxDeadline)
	//	defer cancel()

	start := time.Now()
	id, err := uuid.New()
	if err != nil {
		return err
	}

	_, err = c.MongoClient.Database(mdb).Collection("combinations").UpdateOne(
		context.TODO(),
		bson.M{
			"players": players,
			"size":    len(players),
		},
		[]bson.M{
			{
				"$set": bson.M{
					"_id": bson.M{
						"$cond": []interface{}{bson.M{
							"$eq": []interface{}{bson.M{"$type": "$_id"}, "missing"}},
							id,
							"$_id",
						},
					},
					"players": players,
					"size":    len(players),
					"minutes": bson.M{
						"$add": []interface{}{bson.M{
							"$cond": []interface{}{bson.M{
								"$eq": []interface{}{bson.M{"$type": "$minutes"}, "missing"}},
								0,
								"$minutes",
							}},
							min,
						},
					},
					"cumulated_score": bson.M{
						"$add": []interface{}{bson.M{
							"$cond": []interface{}{bson.M{
								"$eq": []interface{}{bson.M{"$type": "$cumulated_score"}, "missing"}},
								0,
								"$cumulated_score",
							}},
							score,
						},
					},
				},
			},
			{
				"$set": bson.M{
					"score": bson.M{"$divide": []interface{}{"$cumulated_score", "$minutes"}},
				},
			},
		},
		options.Update().SetUpsert(true),
	)

	c.TimeMutex.Lock()
	c.TimeSpent["mongo"] += time.Since(start)
	c.TimeMutex.Unlock()

	return err
}

func (c *Context) InsertGameInfo(competition, url string, home, away []string, goals []Goal, subs []Substitution) error {
	//	ctx, cancel := context.WithTimeout(context.Background(), ctxDeadline)
	//	defer cancel()

	start := time.Now()
	_, err := c.MongoClient.Database(mdb).Collection("games").InsertOne(
		context.TODO(),
		bson.M{
			"_id":         url,
			"competition": competition,
			"home":        home,
			"away":        away,
			"goals":       goals,
			"subs":        subs,
		},
	)

	c.TimeMutex.Lock()
	c.TimeSpent["mongo"] += time.Since(start)
	c.TimeMutex.Unlock()

	return err
}

func (c *Context) InsertCompetition(url string) error {
	//	ctx, cancel := context.WithTimeout(context.Background(), ctxDeadline)
	//	defer cancel()

	_, err := c.MongoClient.Database(mdb).Collection("competitions").InsertOne(
		context.TODO(),
		bson.M{"_id": url},
	)
	return err
}
