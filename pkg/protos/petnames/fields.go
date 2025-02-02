package petnames

const (
	dhtPrefix = "otter:petnames"

	fieldFullName  = "fullname"
	fieldFirstName = "fistname"
	fieldLastName  = "lastname"
)

var (
	allFields = []string{
		fieldFullName,
		fieldFirstName,
		fieldLastName,
	}
)
