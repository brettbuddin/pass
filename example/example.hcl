upstream "hello" {
    destination = "http://hello.local" 
    owner = "Team A <team-a@company.com>"

    route {
        methods = ["GET"]
        path = "/hello"
    }
}
