package blog

/*
// Entry implementation

func (e *Entry) Indexes() [][]string {
	return [][]string{
		[]string{"checksum"},
		[]string{"timestamp"},
	}
}

func (e *Entry) Sorting() string    { return "-timestamp" }
func (e *Entry) Collection() string { return "stream" }
func (e *Entry) PreSave()           {}
func (e *Entry) Unique() bson.M {
	if len(e.Id) > 0 {
		return bson.M{"_id": e.Id}
	}
	return bson.M{"slug": e.SourceId, "type": e.Type}
}

func (e *Entry) SummaryRender() string {
	if len(e.SummaryRendered) > 0 && !conf.Config.Debug {
		return e.SummaryRendered
	}
	var ret string
	var data obj
	b := []byte(e.Data)
	json.Unmarshal(b, &data)

	template_name := fmt.Sprintf("blog/stream/%s-summary.mandira", e.Type)

	if e.Type == "twitter" {
		ret = template.Render(template_name, obj{"Entry": e, "Tweet": data["tweet"]})
	} else if e.Type == "github" {
		event := data["event"].(map[string]interface{})
		var hash string
		if event["id"] != nil {
			hash = event["id"].(string)[:8]
		} else if event["sha"] != nil {
			hash = event["sha"].(string)[:8]
		} else {
			hash = "unknown"
		}
		eventType := event["event"].(string)
		isCommit := eventType == "commit"
		isCreate := eventType == "create"
		isFork := eventType == "fork"
		ret = template.Render(template_name, obj{
			"Entry":    e,
			"Event":    event,
			"Hash":     hash,
			"IsCommit": isCommit,
			"IsCreate": isCreate,
			"IsFork":   isFork,
		})
	} else if e.Type == "bitbucket" {
		// TODO: check username (author) against configured bitbucket username
		update := data["update"].(map[string]interface{})
		revision := fmt.Sprintf("#%d", update["revision"].(float64))
		var repository obj
		if data["repository"] != nil {
			repository = data["repository"].(obj)
		}
		ret = template.Render(template_name, obj{
			"Entry":      e,
			"Data":       data,
			"Update":     update,
			"Repository": repository,
			"Revision":   revision})
	}
	e.SummaryRendered = ret
	if !conf.Config.Debug {
		db.Upsert(e)
	}
	return ret
}
*/
