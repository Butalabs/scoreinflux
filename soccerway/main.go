package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"scoreinflux/db"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "net/http/pprof"

	"github.com/cannona/choose"
	"github.com/go-rod/rod"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

//const ctxDeadline = 30 * time.Second
const urlPrefix = "https://fr.soccerway.com"
const mdb = "xstats"

type Context struct {
	Browser      *rod.Browser
	MongoClient  *mongo.Client
	Mutex        map[string]*sync.Mutex
	MapMutex     *sync.Mutex
	TimeMutex    *sync.Mutex
	Competition  string
	Competitions []string
	TimeSpent    map[string]time.Duration
}

func main() {
	go func() {
		log.Println(http.ListenAndServe("localhost:8095", nil))
	}()

	browser := rod.New().MustConnect()
	defer browser.MustClose()
	//	ctx, cancel := context.WithTimeout(context.Background(), ctxDeadline)
	//	defer cancel()
	client, err := mongo.Connect(context.TODO(), options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		fmt.Println("mongo error", err)
		return
	}

	c := &Context{
		Browser:     browser,
		MongoClient: client,
		//Mutex:       map[string]*sync.Mutex{},
		//MapMutex:    &sync.Mutex{},
		TimeMutex: &sync.Mutex{},
		Competitions: []string{
			"/national/portugal/portuguese-liga-/20202021/regular-season/r59188/",
			"/national/portugal/portuguese-liga-/20192020/regular-season/r53517/",
			"/national/portugal/portuguese-liga-/20182019/regular-season/r47741/",
			"/national/netherlands/eredivisie/20202021/regular-season/r57990/",
			"/national/netherlands/eredivisie/20192020/regular-season/r54058/",
			"/national/netherlands/eredivisie/20182019/regular-season/r47971/",
		},
		TimeSpent: map[string]time.Duration{
			"rod":   0,
			"mongo": 0,
			"total": 0,
		},
	}

	start := time.Now()

	for _, competition := range c.Competitions {
		c.Competition = competition
		var check struct {
			ID string `bson:"_id"`
		}
		err = client.Database(mdb).Collection("competitions").FindOne(context.TODO(), bson.M{"_id": c.Competition}).Decode(&check)
		if err == nil {
			fmt.Println("duplicate")
			return
		}
		if err != mongo.ErrNoDocuments {
			fmt.Println("cannot connect to mongodb", err)
			return
		}

		fail := true
		var urls []string
		for fail {
			urls = c.getCompetitionGames(urlPrefix + c.Competition)
			checkDuplicates := map[string]interface{}{}
			fmt.Println(c.Competition)
			fmt.Println(len(urls))

			for _, url := range urls {
				if _, ok := checkDuplicates[url]; ok {
					fmt.Println("fail")
					fail = true
					break
				} else {
					fail = false
					checkDuplicates[url] = nil
				}
			}
		}

		pool := rod.NewPagePool(6)
		create := func() *rod.Page {
			return browser.MustIncognito().MustPage()
		}

		var wg sync.WaitGroup
		for i, _ := range urls {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				page := pool.Get(create)
				defer pool.Put(page)

				start := time.Now()
				page.MustNavigate(urls[i]).MustWaitLoad()
				c.TimeMutex.Lock()
				c.TimeSpent["rod"] += time.Since(start)
				c.TimeMutex.Unlock()
				err := c.updateWithGame(page, urls[i])
				if err != nil {
					fmt.Println(err)
				}
			}(i)
		}

		wg.Wait()

		err = c.InsertCompetition(c.Competition)
		if err != nil {
			panic(err)
		}
	}

	c.TimeSpent["total"] += time.Since(start)
	fmt.Println(c.TimeSpent)
}

func (c *Context) getCompetitionGames(url string) []string {
	page := c.Browser.MustPage(url)
	defer page.Close()
	content := page.MustElement(".block_competition_matches_summary")

	allMatches := []string{}

	selector := content.MustElement("select")
	for _, val := range selector.MustElements("option") {
		selector.MustSelect(val.MustText())
		content = page.MustElement(".block_competition_matches_summary")
		time.Sleep(1000 * time.Millisecond)
		matches := content.MustElement("tbody").MustElements(".match")
		for _, match := range matches {
			m := match.MustElement(".score-time").MustElement("a").MustAttribute("href")
			if m == nil {
				continue
			}
			allMatches = append(allMatches, urlPrefix+*m)
		}
	}

	return allMatches
}

func computeShaps(dbAll map[string]*db.CombinValue) []string {
	allPlayers := []string{}
	for _, combin := range dbAll {
		if len(combin.Players) == 1 {
			allPlayers = append(allPlayers, combin.Players[0])
		}
	}

	for _, p := range allPlayers {
		val := 0.0
		count := 0
		for _, combin := range dbAll {
			i := -1
			if len(combin.Players) == 2 && combin.Players[0] == p {
				i = 0
			}
			if len(combin.Players) == 2 && combin.Players[1] == p {
				i = 1
			}
			if i == -1 {
				continue
			}

			val += combin.Score - dbAll[combin.Players[1-i]].Score
			count++
		}

		val += float64(count) * dbAll[p].Score
		val /= float64(count * 2)

		dbAll[p].ShapleyValue = val
	}

	sort.Slice(allPlayers, func(i, j int) bool {
		return dbAll[allPlayers[i]].ShapleyValue > dbAll[allPlayers[j]].ShapleyValue
	})

	return allPlayers
}

func (c *Context) updateWithGame(page *rod.Page, url string) error {
	fmt.Println(url)

	content := page.MustElement(".content-column")
	lineupsElems := content.MustElements(".combined-lineups-container")
	if len(lineupsElems) != 2 {
		return errors.New("$$$$$***** INVESTIGATE *****$$$$$ " + url)
	}

	allGoals := []Goal{}
	allSubs := []Substitution{}
	totalTime := 90

	home, goals, subs, totime, err := parseLuElem(lineupsElems[0], true)
	if err != nil {
		return err
	}
	allGoals = append(allGoals, goals...)
	allSubs = append(allSubs, subs...)
	if totime > totalTime {
		totalTime = totime
	}

	away, goals, subs, totime, err := parseLuElem(lineupsElems[0], false)
	if err != nil {
		return err
	}
	allGoals = append(allGoals, goals...)
	allSubs = append(allSubs, subs...)
	if totime > totalTime {
		totalTime = totime
	}

	_, goals, subs, totime, err = parseLuElem(lineupsElems[1], true)
	if err != nil {
		return err
	}
	allGoals = append(allGoals, goals...)
	allSubs = append(allSubs, subs...)
	if totime > totalTime {
		totalTime = totime
	}

	_, goals, subs, totime, err = parseLuElem(lineupsElems[1], false)
	if err != nil {
		return err
	}
	allGoals = append(allGoals, goals...)
	allSubs = append(allSubs, subs...)
	if totime > totalTime {
		totalTime = totime
	}

	sort.Slice(allSubs, func(i, j int) bool {
		return allSubs[i].Min < allSubs[j].Min
	})
	sort.Slice(allGoals, func(i, j int) bool {
		return allGoals[i].Min < allGoals[j].Min
	})

	err = c.InsertGameInfo(c.Competition, url, home, away, allGoals, allSubs)
	if err != nil {
		return errors.Wrap(err, "could not insert game in db")
	}

	i := 0
	lastSub := 0
	for _, sub := range allSubs {
		score := 0
		for i < len(allGoals) && allGoals[i].Min <= sub.Min { // debate if <
			if allGoals[i].Home {
				score++
			} else {
				score--
			}
			i++
		}

		if sub.Min-lastSub > 0 {
			err := c.UpdateScores(home, away, sub.Min-lastSub, float64(score))
			if err != nil {
				return errors.Wrap(err, "cannot update score")
			}
			lastSub = sub.Min
		}
		if sub.Home {
			for j, p := range home {
				if p == sub.Out {
					home[j] = sub.In
				}
			}
		} else {
			for j, p := range away {
				if p == sub.Out {
					away[j] = sub.In
				}
			}
		}
	}

	score := 0
	for i < len(allGoals) {
		if allGoals[i].Home {
			score++
		} else {
			score--
		}
		i++
	}

	return errors.Wrap(c.UpdateScores(home, away, totalTime-lastSub, float64(score)), "cannot update score")
}

func parseLuElem(luElem *rod.Element, home bool) ([]string, []Goal, []Substitution, int, error) {
	selector := ".right"
	if home {
		selector = ".left"
	}

	lu := make([]string, 0, 12)
	goals := []Goal{}
	subs := []Substitution{}
	i := 0
	latestEvent := 90

	for _, player := range luElem.MustElement(selector).MustElement("tbody").MustElements("tr") {
		if i > 11 {
			continue
		}

		if b, _, err := player.Has("a"); !b || err != nil {
			continue
		}

		id := player.MustElement("a").MustAttribute("href")
		if id == nil {
			return nil, nil, nil, 0, errors.New("wtf id null for a player")
		}
		lu = append(lu, *id)
		i++

		if ok, _, _ := player.Has(".substitute-out"); ok {
			out := player.MustElement(".substitute-out")
			outElems := strings.Split(out.MustText(), " ")
			min, err := getMinute(outElems[len(outElems)-1])
			if min >= latestEvent {
				latestEvent = min + 1
			}
			if err != nil {
				return nil, nil, nil, 0, errors.Wrap(err, "cannot parse sub time")
			}
			idOut := out.MustElement("a").MustAttribute("href")
			if idOut == nil {
				return nil, nil, nil, 0, errors.New("wtf id null for a player out")
			}
			subs = append(subs, Substitution{
				Home: home,
				Min:  min,
				In:   *id,
				Out:  *idOut,
			})
		}

		if ok, _, _ := player.Has(".bookings"); !ok {
			continue
		}

		for _, event := range player.MustElement(".bookings").MustElements("span") {
			img := event.MustElement("img").MustAttribute("src")
			if img == nil {
				continue
			}

			min, err := getMinute(event.MustText())
			if err != nil {
				return nil, nil, nil, 0, errors.Wrap(err, "cannot parse event time")
			}
			if min >= latestEvent {
				latestEvent = min + 1
			}

			if *img == "/media/v1.7.6/img/events/OG.png" {
				goals = append(goals, Goal{Home: !home, Min: min})
			}
			if *img == "/media/v1.7.6/img/events/G.png" {
				goals = append(goals, Goal{Home: home, Min: min})
			}
			if *img == "/media/v2.7.6/img/events/RC.png" || *img == "/media/v2.7.6/img/events/Y2C.png" {
				subs = append(subs, Substitution{
					Home: home, Min: min, Out: *id,
				})
			}
		}
	}

	return lu, goals, subs, latestEvent, nil
}

func getMinute(txt string) (int, error) {
	trimmed := strings.TrimRight(strings.TrimLeft(txt, " "), "'")
	extra := strings.Split(trimmed, "+")
	res := 0
	for _, x := range extra {
		val, err := strconv.Atoi(x)
		if err != nil {
			return 0, err
		}
		res += val
	}
	return res, nil
}

func (c *Context) UpdateScores(home []string, away []string, min int, score float64) error {
	homePairs := genPairs(home)
	awayPairs := genPairs(away)
	var err error

	for _, h := range home {
		// _, ok := c.Mutex[h]
		// if !ok {
		// 	c.MapMutex.Lock()
		// 	c.Mutex[h] = &sync.Mutex{}
		// 	c.MapMutex.Unlock()
		// }
		// c.Mutex[h].Lock()
		err = c.InsertOrUpdateCombin([]string{h}, min, score, true)
		if err != nil {
			return errors.Wrap(err, "error with home player score update")
		}
		// c.Mutex[h].Unlock()
	}

	for _, a := range away {
		// _, ok := c.Mutex[a]
		// if !ok {
		// 	c.MapMutex.Lock()
		// 	c.Mutex[a] = &sync.Mutex{}
		// 	c.MapMutex.Unlock()
		// }
		// c.Mutex[a].Lock()
		err = c.InsertOrUpdateCombin([]string{a}, min, score, false)
		if err != nil {
			return errors.Wrap(err, "error with away player score update")
		}
		// c.Mutex[a].Unlock()
	}

	for _, hp := range homePairs {
		// h := hp[0] + hp[1]
		// _, ok := c.Mutex[h]
		// if !ok {
		// 	c.MapMutex.Lock()
		// 	c.Mutex[h] = &sync.Mutex{}
		// 	c.MapMutex.Unlock()
		// }
		// c.Mutex[h].Lock()
		err = c.InsertOrUpdateCombin(hp, min, score, true)
		if err != nil {
			return errors.Wrap(err, "error with home players score update")
		}
		// c.Mutex[h].Unlock()
	}

	for _, ap := range awayPairs {
		// a := ap[0] + ap[1]
		// _, ok := c.Mutex[a]
		// if !ok {
		// 	c.MapMutex.Lock()
		// 	c.Mutex[a] = &sync.Mutex{}
		// 	c.MapMutex.Unlock()
		// }
		// c.Mutex[a].Lock()
		err = c.InsertOrUpdateCombin(ap, min, score, false)
		if err != nil {
			return errors.Wrap(err, "error with away players score update")
		}
		// c.Mutex[a].Unlock()
	}

	return nil
}

func genPairs(elems []string) [][]string {
	size := choose.Choose(int64(len(elems)), 2)
	res := make([][]string, 0, size)
	sort.Strings(elems)
	for i, elem1 := range elems {
		for _, elem2 := range elems[i+1:] {
			res = append(res, []string{elem1, elem2})
		}
	}

	return res
}

type Goal struct {
	Home bool
	Min  int
}

type Substitution struct {
	Home bool
	Min  int
	In   string
	Out  string
}
