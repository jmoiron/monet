package db

import (
	"fmt"
	"github.com/jmoiron/monet/conf"
	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"
)

type Connection struct {
	Session *mgo.Session
	Db      *mgo.Database
	Url     string
	Host    string
	Port    int
}

// A thin Model interface.  Implementing this interface will allow a number
// of simplifications to be executed on that model, like applying default
// ordering, atomatically creating indexes, etc.  Though not necessary to
// use the interface, you should add an empty instance of your model to
// db.Models, which will auto-register indexes at connection time.
type Model interface {
	Collection() string
	Indexes() [][]string
}

type OrderedModel interface {
	Model
	Sorting() string
}

type Updatable interface {
	Model
	Unique() bson.M
	PreSave()
}

func RegisterAllIndexes() {
	for _, m := range Models {
		RegisterIndexes(m)
	}
}

func RegisterIndexes(m Model) {
	collection := Current.Db.C(m.Collection())
	indexes := m.Indexes()
	for _, v := range indexes {
		err := collection.EnsureIndex(mgo.Index{Key: v})
		if err != nil {
			panic(err)
		}
	}
}

func Cursor(m Model) *mgo.Collection {
	return Current.Db.C(m.Collection())
}

func Find(m Model, query interface{}) *mgo.Query {
	cursor := Cursor(m)
	return cursor.Find(query)
}

func Latest(o OrderedModel, query interface{}) *mgo.Query {
	return Find(o.(Model), query).Sort(o.Sorting())
}

func Exists(u Updatable) bool {
	var data interface{}
	err := Current.Db.C(u.Collection()).Find(u.Unique()).One(&data)
	if err != nil {
		return false
	}
	return true
}

func Upsert(u Updatable) (info *mgo.ChangeInfo, err error) {
	u.PreSave()
	return Current.Db.C(u.Collection()).Upsert(u.Unique(), u)
}

var Current = new(Connection)
var Models = []Model{}

func Register(m Model) {
	Models = append(Models, m)
}

// Connect to an mgo url
func Connect(url, database string) {
	session, err := mgo.Dial(url)
	if err != nil {
		panic(err)
	}
	db := session.DB(database)

	Current.Session = session
	Current.Db = db

	for _, m := range Models {
		RegisterIndexes(m)
	}

	fmt.Printf("Connected to mongodb on %s, using \"%s\"\n", url, conf.Config.DbName)
}
