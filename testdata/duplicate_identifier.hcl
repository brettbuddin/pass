upstream "widgets" {
    destination = "http://widgets.local" 
    owner = "Team A <team-a@company.com>"

    route {
        methods = ["GET"]
        path = "/widgets"
    }
}

// Duplicate identifier here.
upstream "widgets" {
    destination = "http://bobs.local"
    owner = "Team B <team-b@company.com>"

    route {
        methods = ["GET"]
        path = "/bobs"
    }
}
