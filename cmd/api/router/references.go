package router

func ref(id string) Reference {
	return Reference{Id: id}
}

func refs(ids []string) []Reference {
	out := make([]Reference, len(ids))
	for i, id := range ids {
		out[i] = ref(id)
	}
	return out
}

func ids(references []Reference) []string {
	out := make([]string, len(references))
	for i, reference := range references {
		out[i] = reference.Id
	}
	return out
}
